package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
func unpack(t *testing.T, name string) map[string]archiveEntry {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := map[string]archiveEntry{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		files[h.Name] = archiveEntry{h.Name, h.Mode, data}
	}
	return files
}
func TestIndependentSkillAndCLIPackages(t *testing.T) {
	root, dist, out := t.TempDir(), t.TempDir(), t.TempDir()
	names := []string{"swe-adoption", "swe-business-model"}
	for _, name := range names {
		put(t, filepath.Join(root, "skills", name, "SKILL.md"), "entry "+name)
		put(t, filepath.Join(root, "skills", name, "references", "rule.md"), "rule")
		put(t, filepath.Join(root, "skills", name, "bin", "stale"), "binary")
		put(t, filepath.Join(root, "skills", name, "runtime.json"), "old")
	}
	for _, platform := range platforms {
		name := "swe_" + strings.ReplaceAll(platform, "/", "_")
		if strings.HasPrefix(platform, "windows/") {
			name += ".exe"
		}
		put(t, filepath.Join(dist, name), "binary "+platform)
	}
	if err := buildPackages(root, dist, out, "cli-version"); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		files := unpack(t, filepath.Join(out, name+".tar.gz"))
		if len(files) != 2 || string(files["skills/"+name+"/SKILL.md"].data) != "entry "+name {
			t.Fatalf("skill files: %v", files)
		}
	}
	for _, platform := range platforms {
		archive := filepath.Join(out, "swe_"+strings.ReplaceAll(platform, "/", "_")+".tar.gz")
		files := unpack(t, archive)
		exe := "swe/bin/swe"
		if strings.HasPrefix(platform, "windows/") {
			exe += ".exe"
		}
		if len(files) != 2 || string(files[exe].data) != "binary "+platform || files[exe].mode&0111 == 0 {
			t.Fatalf("CLI files: %v", files)
		}
		var binding struct {
			Version string                       `json:"version"`
			Assets  map[string]map[string]string `json:"assets"`
		}
		if err := json.Unmarshal(files["swe/runtime.json"].data, &binding); err != nil {
			t.Fatal(err)
		}
		if binding.Version != "cli-version" || binding.Assets[platform]["sha256"] != fmt.Sprintf("%x", sha256.Sum256(files[exe].data)) {
			t.Fatalf("binding: %+v", binding)
		}
		sums, err := os.ReadFile(filepath.Join(out, "SHA256SUMS"))
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(archive)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(sums), fmt.Sprintf("%x  %s", sha256.Sum256(data), filepath.Base(archive))) {
			t.Fatal("checksum mismatch")
		}
	}
}
func TestSingleSkillBuildNeedsNoSiblingsOrCLI(t *testing.T) {
	root, out := filepath.Join(t.TempDir(), "swe-testing"), t.TempDir()
	put(t, filepath.Join(root, "SKILL.md"), "independent")
	if err := buildSkillPackage(root, filepath.Join(out, "swe-testing.tar.gz")); err != nil {
		t.Fatal(err)
	}
	if len(unpack(t, filepath.Join(out, "swe-testing.tar.gz"))) != 1 {
		t.Fatal("unexpected files")
	}
}
func TestMissingBinaryDoesNotProduceCLIArchive(t *testing.T) {
	out := t.TempDir()
	if err := buildCLIPackage(filepath.Join(t.TempDir(), "missing"), filepath.Join(out, "cli.tar.gz"), "v1", "darwin/arm64", "swe"); err == nil {
		t.Fatal("missing binary accepted")
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 0 {
		t.Fatalf("partial archive: %v", entries)
	}
}
