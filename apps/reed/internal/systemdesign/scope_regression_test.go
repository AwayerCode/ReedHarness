package systemdesign

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Deterministic read-error injection also exercises platforms/users for which
// chmod cannot make a directory unreadable. No global filesystem hooks are used.
func TestDiscoveryReadErrorsPreserveOtherScopes(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{Filename, "a/" + Filename, "b/" + Filename, "c/" + Filename} {
		write(t, root, rel, "schema_version: 1\n")
	}
	blocked := filepath.Join(root, "a", "blocked")
	if err := os.Mkdir(blocked, 0755); err != nil {
		t.Fatal(err)
	}
	walk := func(root string, visit fs.WalkDirFunc) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if p == blocked {
				if err := visit(p, d, &os.PathError{Op: "readdir", Path: p, Err: fs.ErrPermission}); err != nil {
					return err
				}
				return filepath.SkipDir
			}
			return visit(p, d, e)
		})
	}
	all, err := discover(root, walk)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Scopes) != 4 || len(all.Issues) != 1 {
		t.Fatalf("%+v", all)
	}
	for _, s := range all.Scopes {
		if (s.Root == filepath.Join(root, "a")) != (s.Unavailable != "") {
			t.Fatalf("wrong issue owner: %+v", s)
		}
		one, err := discover(s.Root, walk)
		if err != nil {
			t.Fatal(err)
		}
		var matching Scope
		for _, candidate := range one.Scopes {
			if candidate.File == s.File {
				matching = candidate
			}
		}
		if !reflect.DeepEqual(s, matching) {
			t.Fatalf("single/all differ: %+v / %+v", s, matching)
		}
	}
}

func TestDiscoveryUnownedReadErrorIsReported(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "ok/"+Filename, "schema_version: 1\n")
	result, err := discover(root, func(root string, visit fs.WalkDirFunc) error {
		if err := visit(filepath.Join(root, "unknown"), nil, fs.ErrPermission); err != nil {
			return err
		}
		return filepath.WalkDir(root, visit)
	})
	if err != nil || len(result.Issues) != 1 || len(result.Scopes) != 1 || result.Scopes[0].Unavailable != "" {
		t.Fatalf("%+v %v", result, err)
	}
}
