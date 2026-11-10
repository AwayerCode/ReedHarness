package installer

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill is one installed or packaged entry: its directory name, project-relative
// path and the summary declared in SKILL.md.
type Skill struct {
	Name    string
	Path    string
	Summary string
}

func parseSkill(data []byte, location string) (Skill, error) {
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
	return Skill{Name: name, Path: location, Summary: summary}, nil
}
