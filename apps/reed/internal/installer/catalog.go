package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/AwayerCode/ReedHarness/apps/reed/internal/source"
)

type Skill struct {
	Name    string
	Path    string
	Summary string
}

func (m Manager) Search(ctx context.Context, rawSource, version, query string) ([]Skill, error) {
	if m.Client == nil {
		return nil, errors.New("HTTP client is required")
	}
	if rawSource == "" {
		return nil, errors.New("source is required")
	}
	if version == "" {
		version = "branch:main"
	}
	archiveURL, err := source.ResolveArchiveURL(rawSource, version)
	if err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp("", "reed-search-*")
	if err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	contentRoot := filepath.Join(stage, "content")
	if err := source.Fetch(ctx, m.Client, archiveURL, []string{"skills/index.json"}, contentRoot); err != nil {
		return nil, fmt.Errorf("read skills/index.json: %w", err)
	}
	skills, err := readCatalog(contentRoot)
	if err != nil {
		return nil, err
	}

	needle := strings.ToLower(strings.TrimSpace(query))
	matches := make([]Skill, 0, len(skills))
	for _, skill := range skills {
		if needle == "" || matchesQuery(skill, needle) {
			matches = append(matches, skill)
		}
	}
	sort.Slice(matches, func(left, right int) bool {
		return matches[left].Name < matches[right].Name
	})
	return matches, nil
}

// Search reads only the catalog; repository tests verify it against SKILL.md.
func readCatalog(root string) ([]Skill, error) {
	data, err := os.ReadFile(filepath.Join(root, "skills", "index.json"))
	if err != nil {
		return nil, err
	}
	var catalog struct {
		SchemaVersion int `json:"schema_version"`
		Skills        []struct {
			Name        string `json:"name"`
			Path        string `json:"path"`
			Description string `json:"description"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parse skills/index.json: %w", err)
	}
	if catalog.SchemaVersion != 1 || catalog.Skills == nil {
		return nil, errors.New("skills/index.json requires schema_version 1 and a skills array")
	}
	skills := make([]Skill, 0, len(catalog.Skills))
	names := map[string]bool{}
	validName := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	for _, entry := range catalog.Skills {
		if !validName.MatchString(entry.Name) || len(entry.Name) > 64 || strings.TrimSpace(entry.Description) == "" {
			return nil, fmt.Errorf("invalid skill entry %q: lowercase name and description required", entry.Name)
		}
		if names[entry.Name] {
			return nil, fmt.Errorf("duplicate skill name %q", entry.Name)
		}
		names[entry.Name] = true
		location := entry.Path
		if location == "" {
			location = "skills/" + entry.Name
		}
		if location != "skills/"+entry.Name && location != "skills/3rdparty/"+entry.Name {
			return nil, fmt.Errorf("invalid catalog path %q for %q", location, entry.Name)
		}
		if entry.Name == "3rdparty" {
			return nil, errors.New("3rdparty is a reserved grouping directory")
		}
		skills = append(skills, Skill{Name: entry.Name, Path: location, Summary: strings.Join(strings.Fields(entry.Description), " ")})
	}
	return skills, nil
}

func parseSkill(data []byte, path string) (Skill, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return Skill{}, errors.New("missing YAML front matter")
	}
	end := 1
	for end < len(lines) && lines[end] != "---" {
		end++
	}
	if end == len(lines) {
		return Skill{}, errors.New("unclosed YAML front matter")
	}
	var metadata struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &metadata); err != nil {
		return Skill{}, fmt.Errorf("parse YAML front matter: %w", err)
	}
	name := strings.TrimSpace(metadata.Name)
	summary := strings.Join(strings.Fields(metadata.Description), " ")
	if name == "" || summary == "" {
		return Skill{}, errors.New("front matter requires name and description")
	}
	return Skill{Name: name, Path: path, Summary: summary}, nil
}

func matchesQuery(skill Skill, needle string) bool {
	return strings.Contains(strings.ToLower(skill.Name), needle) ||
		strings.Contains(strings.ToLower(skill.Path), needle) ||
		strings.Contains(strings.ToLower(skill.Summary), needle)
}
