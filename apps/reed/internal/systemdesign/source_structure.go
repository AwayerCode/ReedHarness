package systemdesign

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// regularPath rejects symlinks in every component, including source_root ancestors.
func regularPath(root, rel string) (fs.FileInfo, error) {
	if !SafePath(rel) {
		return nil, fmt.Errorf("unsafe path: %s", rel)
	}
	current := root
	var info fs.FileInfo
	for _, part := range strings.Split(rel, "/") {
		current = filepath.Join(current, part)
		var e error
		info, e = os.Lstat(current)
		if e != nil {
			return nil, e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink not supported: %s", current)
		}
	}
	return info, nil
}
func (d *Design) owner(rel string) *Module {
	for i := range d.Modules {
		p := d.ModulePath(d.Modules[i])
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return &d.Modules[i]
		}
	}
	return nil
}

type sourceFile struct {
	Path     string
	Relative string
	Owner    *Module
}

// sourceInventory is the language-neutral result of inspecting the managed
// source tree. Language adapters consume it only after structural rules pass.
type sourceInventory struct {
	Root        string
	Scope       Scope
	Files       []sourceFile
	Directories []string
}

func (d *Design) checkRequiredPaths(root string, r *Report) error {
	var e error
	requiredDir := func(rel string) error {
		info, e := regularPath(root, rel)
		if os.IsNotExist(e) {
			r.Add("structure.missing", rel, "required directory missing")
			return nil
		}
		if e != nil {
			return e
		}
		if !info.IsDir() {
			r.Add("structure.directory", rel, "expected directory")
		}
		return nil
	}
	if e = requiredDir(d.Source); e != nil {
		return e
	}
	for _, l := range d.Layers {
		if e = requiredDir(path.Join(d.Source, l.Directory)); e != nil {
			return e
		}
	}
	for _, m := range d.Modules {
		p := d.ModulePath(m)
		if e = requiredDir(p); e != nil {
			return e
		}
		for _, f := range m.Required {
			rel := path.Join(p, f)
			info, e := regularPath(root, rel)
			if os.IsNotExist(e) {
				r.Add("structure.missing", rel, "required file missing")
				continue
			}
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				r.Add("structure.file", rel, "expected regular file")
			}
		}
	}
	return nil
}

// inspectSource validates all language-neutral directory and file ownership.
// It deliberately performs no language-specific parsing.
func (d *Design) inspectSource(root string, scope Scope, r *Report) (sourceInventory, error) {
	inventory := sourceInventory{Root: root, Scope: scope, Files: []sourceFile{}, Directories: []string{}}
	diagnosticsBefore := len(r.Diagnostics)
	if err := d.checkRequiredPaths(root, r); err != nil {
		return inventory, err
	}
	if len(r.Diagnostics) > 0 {
		return inventory, nil
	}
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(d.Source)), func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if scope.Delegated(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not supported: %s", rel)
		}
		if entry.IsDir() {
			if rel == d.Source {
				return nil
			}
			inventory.Directories = append(inventory.Directories, rel)
			valid := false
			for _, l := range d.Layers {
				if rel == path.Join(d.Source, l.Directory) {
					valid = true
				}
			}
			if !valid && d.owner(rel) == nil {
				r.Add("structure.unowned", rel, "undeclared directory outside modules")
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular file: %s", rel)
		}
		owner := d.owner(rel)
		inventory.Files = append(inventory.Files, sourceFile{Path: p, Relative: rel, Owner: owner})
		if owner == nil {
			r.Add("structure.unowned", rel, "file has no module owner")
			return nil
		}
		return nil
	})
	if err == nil && len(r.Diagnostics) == diagnosticsBefore {
		r.Coverage = append(r.Coverage, fmt.Sprintf("%d regular files remained inside owned modules; child design scopes excluded", len(inventory.Files)))
	}
	return inventory, err
}
