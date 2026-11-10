package systemdesign

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxLanguageSourceBytes = 16 << 20

func readLanguageSource(file string) ([]byte, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source is not a regular file: %s", file)
	}
	if info.Size() > maxLanguageSourceBytes {
		return nil, fmt.Errorf("source exceeds %d bytes: %s", maxLanguageSourceBytes, file)
	}
	return os.ReadFile(file)
}

// existingSourceFiles returns deduplicated regular-file matches. Candidate
// paths inside the design scope pass through regularPath so symlinked parents
// cannot hide a managed source target.
func existingSourceFiles(root string, candidates []string) ([]string, error) {
	seen := map[string]bool{}
	matches := []string{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		rel, err := filepath.Rel(root, candidate)
		if err != nil {
			return nil, err
		}
		var info os.FileInfo
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			info, err = os.Lstat(candidate)
		} else {
			info, err = regularPath(root, filepath.ToSlash(rel))
		}
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			matches = append(matches, candidate)
		}
	}
	return matches, nil
}

// checkResolvedSourceFile maps one resolved source reference to its owning
// module and applies the shared YAML dependency policy.
func (d *Design) checkResolvedSourceFile(inventory sourceInventory, from sourceFile, targetAbs, raw string, line int, language string, state *sourceAdapterState, r *Report) error {
	if from.Owner == nil {
		return fmt.Errorf("%s source file has no owner: %s", language, from.Relative)
	}
	rel, err := filepath.Rel(inventory.Root, targetAbs)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	location := fmt.Sprintf("%s:%d", from.Relative, line)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		r.Add("dependency.cross-scope", location, language+" reference leaves this system-design scope: "+raw)
		return nil
	}
	if inventory.Scope.Delegated(rel) {
		r.Add("dependency.cross-scope", location, language+" reference enters a child system-design scope: "+raw)
		return nil
	}
	info, err := regularPath(inventory.Root, rel)
	if os.IsNotExist(err) {
		r.Add("dependency.missing", location, language+" internal reference missing: "+raw)
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		r.Add("dependency.missing", location, language+" internal reference is not a regular file: "+raw)
		return nil
	}
	to := d.owner(rel)
	if to == nil {
		r.Add("dependency.unowned", location, language+" reference targets a file outside managed modules: "+raw)
		return nil
	}
	if to.ID == from.Owner.ID {
		return nil
	}
	recordDependency(from.Owner, to, from.Relative, line, state.observed, r)
	return nil
}

func addMissingLanguageReference(r *Report, file, language, raw string, line int) {
	r.Add("dependency.missing", fmt.Sprintf("%s:%d", file, line), language+" internal reference missing: "+raw)
}

func languageLocation(file string, line int) string {
	return fmt.Sprintf("%s:%d", file, line)
}
