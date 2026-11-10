package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// List discovers local Skills, including ones installed without reed. It does
// not infer their source revision or verify changes against an absent record.
func (m Manager) List() ([]Skill, error) {
	root := filepath.Join(m.ProjectRoot, ".agents", "skills")
	if err := checkParents(m.ProjectRoot, filepath.Join(root, "SKILL.md")); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	skills := []Skill{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		filename := filepath.Join(root, entry.Name(), "SKILL.md")
		info, err := os.Lstat(filename)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Skill entry %q is not a regular file", filename)
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		skill, err := parseSkill(data, filepath.ToSlash(filepath.Join(".agents", "skills", entry.Name())))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		skills = append(skills, skill)
	}
	return skills, nil
}
