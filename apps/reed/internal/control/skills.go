package control

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Version string `json:"version"`
	Content string `json:"content"`
}

func ListSkills(root, version string) ([]Skill, error) {
	result := []Skill{}
	if root == "" {
		return result, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		text, err := ReadSkill(root, e.Name(), "SKILL.md")
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, Skill{e.Name(), root, version, text})
	}
	return result, nil
}
func ReadSkill(root, name, path string) (string, error) {
	if root == "" || !fs.ValidPath(name) || strings.ContainsAny(name, "/\\") || !fs.ValidPath(path) || strings.Contains(path, "\\") {
		return "", fs.ErrInvalid
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	canonicalRoot, err = filepath.Abs(canonicalRoot)
	if err != nil {
		return "", err
	}
	base := filepath.Join(canonicalRoot, name)
	real, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || !filepath.IsLocal(rel) {
		return "", errors.New("Skill resource escapes its source directory")
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return "", errors.New("invalid or oversized Skill resource")
	}
	data, err := os.ReadFile(real)
	return string(data), err
}
