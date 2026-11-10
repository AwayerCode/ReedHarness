package installer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/AwayerCode/ReedHarness/apps/reed/internal/source"
)

type Manager struct {
	ProjectRoot string
	Client      *http.Client
	DryRun      bool
}

type Result struct {
	Path    string
	Version string
	Files   int
}

// Install distributes complete Skills without keeping project ownership records.
// An existing directory is never replaced, even when it was installed by reed.
func (m *Manager) Install(ctx context.Context, rawSource, version string, requested []string) ([]Result, error) {
	if m.Client == nil {
		return nil, errors.New("HTTP client is required")
	}
	targets, err := normalizeTargets(requested)
	if err != nil {
		return nil, err
	}
	archiveURL, err := source.ResolveArchiveURL(rawSource, version)
	if err != nil {
		return nil, err
	}
	skillsRoot := filepath.Join(m.ProjectRoot, ".agents", "skills")
	for _, target := range targets {
		destination := filepath.Join(skillsRoot, path.Base(target))
		if err := checkParents(m.ProjectRoot, destination); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(destination); err == nil {
			return nil, fmt.Errorf("Skill %q already exists; install into a separate directory to compare before replacing it", destination)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	stage, err := os.MkdirTemp("", "reed-install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	content := filepath.Join(stage, "content")
	candidates := append([]string{"skills/index.json"}, targets...)
	for _, target := range targets {
		if strings.Count(target, "/") == 1 && strings.HasPrefix(target, "skills/") {
			candidates = append(candidates, "skills/3rdparty/"+path.Base(target))
		}
	}
	if err := source.FetchOptional(ctx, m.Client, archiveURL, nil, candidates, content); err != nil {
		return nil, err
	}
	targets, err = resolveCatalogTargets(content, targets)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(targets))
	for _, target := range targets {
		root := filepath.Join(content, filepath.FromSlash(target))
		data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
		if err != nil {
			return nil, fmt.Errorf("%q must be a Skill directory with SKILL.md: %w", target, err)
		}
		skill, err := parseSkill(data, target)
		if err != nil {
			return nil, fmt.Errorf("invalid Skill %q: %w", target, err)
		}
		if skill.Name != path.Base(target) {
			return nil, fmt.Errorf("Skill name %q does not match directory %q", skill.Name, target)
		}
		files := 0
		if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("unsupported Skill file %q", name)
			}
			files++
			return nil
		}); err != nil {
			return nil, err
		}
		results = append(results, Result{Path: filepath.ToSlash(filepath.Join(".agents", "skills", skill.Name)), Version: version, Files: files})
	}
	if m.DryRun {
		return results, nil
	}
	createdParents, err := mkdirAllTracked(m.ProjectRoot, skillsRoot)
	if err != nil {
		return nil, err
	}
	installed := []string{}
	rollback := func(cause error) ([]Result, error) {
		for i := len(installed) - 1; i >= 0; i-- {
			if err := os.RemoveAll(installed[i]); err != nil {
				cause = errors.Join(cause, fmt.Errorf("clean partial Skill %q: %w", installed[i], err))
			}
		}
		for i := len(createdParents) - 1; i >= 0; i-- {
			_ = os.Remove(createdParents[i])
		}
		return nil, cause
	}
	for _, target := range targets {
		destination := filepath.Join(skillsRoot, path.Base(target))
		// Mkdir reserves only this Skill, without a persistent lock or overwriting a
		// directory installed concurrently. The entry is copied last for discovery.
		if err := os.Mkdir(destination, 0755); err != nil {
			return rollback(fmt.Errorf("reserve Skill %q: %w", destination, err))
		}
		installed = append(installed, destination)
		if err := copySkill(ctx, filepath.Join(content, filepath.FromSlash(target)), destination); err != nil {
			return rollback(err)
		}
	}
	return results, nil
}

func copySkill(ctx context.Context, sourceRoot, destination string) error {
	var entries []string
	err := filepath.WalkDir(sourceRoot, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(sourceRoot, name)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.IsDir() {
			return os.Mkdir(filepath.Join(destination, relative), 0755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported Skill file %q", name)
		}
		if relative != "SKILL.md" {
			entries = append(entries, relative)
		}
		return nil
	})
	if err != nil {
		return err
	}
	entries = append(entries, "SKILL.md")
	for _, relative := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Stat(filepath.Join(sourceRoot, relative))
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if relative == "SKILL.md" {
			temporary, err := os.CreateTemp(destination, ".skill-entry-*")
			if err != nil {
				return err
			}
			temporaryName := temporary.Name()
			if err := temporary.Close(); err != nil {
				return err
			}
			defer os.Remove(temporaryName)
			if err := copyFile(filepath.Join(sourceRoot, relative), temporaryName, info.Mode()); err != nil {
				return err
			}
			if err := os.Rename(temporaryName, target); err != nil {
				return err
			}
		} else if err := copyFile(filepath.Join(sourceRoot, relative), target, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func normalizeTargets(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return nil, errors.New("at least one --path is required")
	}
	validName := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	seen := map[string]string{}
	targets := []string{}
	for _, raw := range requested {
		clean := strings.TrimSuffix(strings.ReplaceAll(raw, "\\", "/"), "/")
		parts := strings.Split(clean, "/")
		name := path.Base(clean)
		validLayout := (len(parts) == 2 && (parts[0] == "skills" || parts[0] == "Skills")) ||
			(len(parts) == 3 && parts[0] == "skills" && parts[1] == "3rdparty")
		if !validLayout || !validName.MatchString(name) || len(name) > 64 || name == "3rdparty" {
			return nil, fmt.Errorf("invalid Skill path %q; expected skills/<name> or skills/3rdparty/<name> (historical Skills/<name> is also supported)", raw)
		}
		if previous, ok := seen[name]; ok {
			if previous != clean {
				return nil, fmt.Errorf("Skill paths %q and %q have the same destination", previous, clean)
			}
			continue
		}
		seen[name] = clean
		targets = append(targets, clean)
	}
	sort.Strings(targets)
	return targets, nil
}

// Catalog paths are authoritative when present. Legacy flat paths are aliases;
// archives without a catalog (including platform packages) retain direct lookup.
func resolveCatalogTargets(root string, targets []string) ([]string, error) {
	catalog, err := readCatalog(root)
	if errors.Is(err, os.ErrNotExist) {
		return targets, nil
	}
	if err != nil {
		return nil, err
	}
	byName := map[string]string{}
	for _, skill := range catalog {
		byName[skill.Name] = skill.Path
	}
	resolved := append([]string(nil), targets...)
	for i, target := range targets {
		location, ok := byName[path.Base(target)]
		if !ok || strings.HasPrefix(target, "Skills/") {
			continue
		}
		if strings.Count(target, "/") == 1 {
			resolved[i] = location
		} else if location != target {
			return nil, fmt.Errorf("requested path %q differs from catalog path %q", target, location)
		}
	}
	return resolved, nil
}
