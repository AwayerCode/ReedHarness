package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestCompiledResourcesMatchSource(t *testing.T) {
	root := "../../../../skills"
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "SKILL.md")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		names = append(names, entry.Name())
		err := filepath.WalkDir(filepath.Join(root, entry.Name()), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(filepath.Join(root, entry.Name()), path)
			if err != nil {
				return err
			}
			if rel == "bin" && d.IsDir() {
				return filepath.SkipDir
			}
			if rel == "bin" || rel == "runtime.json" || d.IsDir() {
				return nil
			}
			count++
			want, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			got, err := Read(entry.Name(), filepath.ToSlash(rel))
			if err != nil || got != string(want) {
				t.Errorf("compiled resource differs: %s; run go generate ./... (%v)", path, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(names)
	if len(names) == 0 || !reflect.DeepEqual(Names(), names) {
		t.Fatalf("compiled names %v, source %v", Names(), names)
	}
	if count != len(resources) {
		t.Fatalf("compiled resource count %d, source %d", len(resources), count)
	}
}

func TestReadRejectsInvalidOrMissingResource(t *testing.T) {
	for _, pair := range [][2]string{
		{"missing", "SKILL.md"}, {"swe-testing", "missing.md"},
		{"../swe-testing", "SKILL.md"}, {"swe-testing", "../swe-adoption/SKILL.md"},
		{"swe-testing", "/SKILL.md"}, {"swe-testing", "references/../../SKILL.md"},
	} {
		if content, err := Read(pair[0], pair[1]); err == nil || content != "" {
			t.Errorf("read %v: %q %v", pair, content, err)
		}
	}
	for _, name := range Names() {
		text, err := Read(name, "SKILL.md")
		if err != nil || !strings.Contains(text, "name: "+name) {
			t.Fatalf("invalid entry %s: %v", name, err)
		}
	}
}
