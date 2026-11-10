// Package skills exposes the immutable resources compiled from skills/.
package skills

import (
	"io/fs"
	"sort"
	"strings"
)

func Names() []string {
	var names []string
	for path := range resources {
		if name, found := strings.CutSuffix(path, "/SKILL.md"); found && !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Read returns a resource relative to a Skill's root, without disk or network IO.
func Read(name, path string) (string, error) {
	if !fs.ValidPath(name) || strings.Contains(name, "/") || !fs.ValidPath(path) {
		return "", fs.ErrInvalid
	}
	if _, ok := resources[name+"/SKILL.md"]; !ok {
		return "", fs.ErrNotExist
	}
	content, ok := resources[name+"/"+path]
	if !ok {
		return "", fs.ErrNotExist
	}
	return content, nil
}
