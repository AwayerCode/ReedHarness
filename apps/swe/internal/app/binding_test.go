package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRuntimeBinding(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if e := os.Mkdir(bin, 0700); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(bin, "swe")
	write(t, file, "binary")
	binding := func(version, digest string) {
		b, _ := json.Marshal(map[string]any{"schema_version": 1, "name": "swe", "version": version, "assets": map[string]any{runtime.GOOS + "/" + runtime.GOARCH: map[string]string{"sha256": digest}}})
		write(t, filepath.Join(root, "runtime.json"), string(b))
	}
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte("binary")))
	binding(Version, sum)
	if e := verifyBinding(file); e != nil {
		t.Fatal(e)
	}
	binding("old", sum)
	if e := verifyBinding(file); e == nil {
		t.Fatal("wrong version accepted")
	}
	binding(Version, sum)
	write(t, file, "tampered")
	if e := verifyBinding(file); e == nil {
		t.Fatal("tampered executable accepted")
	}
}

func TestMissingRuntimeBinding(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		skill, installed, wantError bool
	}{
		{"standalone", false, false, false},
		{"custom Skill location", true, false, false},
		{"incomplete installed Skill", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.installed {
				root = filepath.Join(root, ".agents", "skills", "swe")
			}
			file := filepath.Join(root, "bin", "swe")
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			write(t, file, "binary")
			if tc.skill {
				write(t, filepath.Join(root, "SKILL.md"), "skill")
			}
			err := verifyBinding(file)
			if (err != nil) != tc.wantError {
				t.Fatalf("binding error: %v", err)
			}
		})
	}
}
