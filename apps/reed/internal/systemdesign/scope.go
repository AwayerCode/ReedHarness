package systemdesign

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const Filename = "system-design.yaml"

type Scope struct {
	Unavailable string   `json:"unavailable,omitempty"`
	File        string   `json:"file"`
	Root        string   `json:"root"`
	Children    []string `json:"excluded_scopes"`
}

type DiscoveryIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Discovery struct {
	Scopes []Scope          `json:"scopes"`
	Issues []DiscoveryIssue `json:"discovery_issues"`
}

// All ordinary directories participate in ownership, including hidden and
// dependency directories. Only Git metadata is excluded from every entry point.
func gitMetadata(p string) bool {
	for _, component := range strings.Split(filepath.ToSlash(filepath.Clean(p)), "/") {
		if component == ".git" {
			return true
		}
	}
	return false
}

func Discover(root string) (Discovery, error) { return discover(root, filepath.WalkDir) }

func discover(root string, walk func(string, fs.WalkDirFunc) error) (Discovery, error) {
	result := Discovery{Scopes: []Scope{}, Issues: []DiscoveryIssue{}}
	root, err := filepath.Abs(root)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	if gitMetadata(root) {
		return result, fmt.Errorf("Git metadata is not a system-design scope: %s", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, fmt.Errorf("discovery root is not a directory: %s", root)
	}
	err = walk(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			result.Issues = append(result.Issues, DiscoveryIssue{Path: p, Message: err.Error()})
			// The walk already stops descending an unreadable directory. Returning nil
			// preserves its siblings even when DirEntry is unavailable.
			return nil
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != Filename {
			return nil
		}
		scope := Scope{File: p, Root: filepath.Dir(p), Children: []string{}}
		if !entry.Type().IsRegular() {
			scope.Unavailable = "design must be a regular file, not a directory or symlink"
		}
		result.Scopes = append(result.Scopes, scope)
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	sort.Slice(result.Scopes, func(i, j int) bool { return result.Scopes[i].File < result.Scopes[j].File })
	sort.Slice(result.Issues, func(i, j int) bool { return result.Issues[i].Path < result.Issues[j].Path })
	for i := range result.Scopes {
		for j := range result.Scopes {
			if i != j && within(result.Scopes[i].Root, result.Scopes[j].Root) {
				result.Scopes[i].Children = append(result.Scopes[i].Children, result.Scopes[j].Root)
			}
		}
	}
	for _, issue := range result.Issues {
		owner := -1
		for i, s := range result.Scopes {
			if (issue.Path == s.Root || within(s.Root, issue.Path)) && (owner < 0 || len(s.Root) > len(result.Scopes[owner].Root)) {
				owner = i
			}
		}
		if owner >= 0 {
			msg := "scope boundary discovery incomplete: " + issue.Path + ": " + issue.Message
			if result.Scopes[owner].Unavailable != "" {
				msg = result.Scopes[owner].Unavailable + "; " + msg
			}
			result.Scopes[owner].Unavailable = msg
		}
	}
	return result, nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func FileScope(file string) (Scope, error) {
	file, err := filepath.Abs(file)
	if err != nil {
		return Scope{}, err
	}
	if filepath.Base(file) != Filename {
		return Scope{}, fmt.Errorf("design filename must be %s", Filename)
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(file))
	if err != nil {
		return Scope{}, err
	}
	scopes, err := Discover(root)
	if err != nil {
		return Scope{}, err
	}
	for _, s := range scopes.Scopes {
		if s.Root == root {
			return s, nil
		}
	}
	return Scope{}, fmt.Errorf("design not discovered: %s", file)
}

func (s Scope) Delegated(rel string) bool {
	p := filepath.Join(s.Root, filepath.FromSlash(rel))
	for _, child := range s.Children {
		if p == child || within(child, p) {
			return true
		}
	}
	return false
}

// Parents may contain a child below a module, but cannot claim anything inside it.
func (d *Design) ValidateScope(s Scope, r *Report) {
	check := func(p string) {
		if s.Delegated(p) {
			r.Add("scope.overlap", p, "path belongs to a child system-design scope")
		}
	}
	check(d.Source)
	for _, l := range d.Layers {
		check(path.Join(d.Source, l.Directory))
	}
	for _, m := range d.Modules {
		check(d.ModulePath(m))
		for _, f := range m.Required {
			check(path.Join(d.ModulePath(m), f))
		}
	}
}
