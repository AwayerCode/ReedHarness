package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Observer binds results to content and lexical-to-real path resolution. It is not a filesystem lock.
type Observer struct {
	Root  string
	Files map[string]string
}

func inside(root, file string) bool {
	rel, err := filepath.Rel(root, file)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func (o *Observer) snapshot(file string) (string, []byte, error) {
	file = filepath.Clean(file)
	if !inside(o.Root, file) {
		return "", nil, errors.New("input outside projectRoot: " + file)
	}
	real, err := filepath.EvalSymlinks(file)
	if os.IsNotExist(err) {
		return "missing", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if !inside(o.Root, real) {
		return "", nil, errors.New("resolved input outside projectRoot: " + file)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", nil, err
	}
	if info.IsDir() {
		return "directory:" + real, nil, nil
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("input is not a regular file: " + file)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return "", nil, err
	}
	return "file:" + real + ":" + observationDigest(data), data, nil
}
func (o *Observer) observe(file string) (string, []byte, error) {
	state, data, err := o.snapshot(file)
	if err != nil {
		return "", nil, err
	}
	if old, ok := o.Files[file]; ok && old != state {
		return "", nil, errors.New("input changed during check: " + file)
	}
	o.Files[file] = state
	return state, data, nil
}
func (o *Observer) Read(file string) (string, error) {
	state, data, err := o.observe(file)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(state, "file:") {
		return "", fmt.Errorf("not a readable file: %s", file)
	}
	if !utf8.Valid(data) {
		return "", errors.New("input is not UTF-8: " + file)
	}
	return string(data), nil
}
func (o *Observer) Exists(file string) (bool, error) {
	state, _, err := o.observe(file)
	return state != "missing", err
}
func (o *Observer) Changed() []string {
	changed := []string{}
	for file, previous := range o.Files {
		now, _, err := o.snapshot(file)
		if err != nil || now != previous {
			changed = append(changed, file)
		}
	}
	slices.Sort(changed)
	return changed
}

func (o *Observer) PathKind(file string) (string, error) {
	state, _, err := o.observe(file)
	if err != nil {
		return "", err
	}
	kind, _, _ := strings.Cut(state, ":")
	return kind, nil
}

func observationDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
