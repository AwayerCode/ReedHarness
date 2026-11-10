package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwayerCode/ReedHarness/apps/swe/internal/systemdesign"
)

const scopedDesign = `schema_version: 1
source_root: src
layers:
  - id: domain
    directory: domain
    responsibility: Domain
    allowed_dependencies: []
modules:
  - id: core
    layer: domain
    directory: core
    responsibility: Core
    required_files: [core.go]
    allowed_dependencies: []
`

func scopeProject(t *testing.T, root, mod string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "src/domain/core"), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "system-design.yaml"), scopedDesign)
	write(t, filepath.Join(root, "src/domain/core/core.go"), "package core\n")
	if mod != "" {
		write(t, filepath.Join(root, "go.mod"), "module "+mod+"\n\ngo 1.24\n")
	}
}

func designRun(t *testing.T, args ...string) (int, []byte) {
	t.Helper()
	var out bytes.Buffer
	code := Run(append([]string{"system-design"}, args...), nil, &out)
	return code, out.Bytes()
}

func TestSystemDesignMonorepoScopes(t *testing.T) {
	t.Setenv("GOWORK", "off")
	repo := t.TempDir()
	write(t, filepath.Join(repo, ".git"), "gitdir: irrelevant\n")
	root := filepath.Join(repo, "apps/api")
	scopeProject(t, root, "example.com/api")
	child := filepath.Join(root, "src/domain/core/child")
	scopeProject(t, child, "")
	sibling := filepath.Join(repo, "apps/worker")
	scopeProject(t, sibling, "example.com/worker")
	code, b := designRun(t, "list", "--root", repo)
	var list designBatch
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if code != 0 || len(list.Scopes) != 3 {
		t.Fatalf("%d %s", code, b)
	}
	for _, mode := range []string{"validate", "check"} {
		code, b = designRun(t, mode, "--root", repo, "--all")
		var batch designBatch
		if err := json.Unmarshal(b, &batch); err != nil {
			t.Fatal(err)
		}
		if code != 0 || len(batch.Results) != 3 {
			t.Fatalf("%d %s", code, b)
		}
		for _, r := range batch.Results {
			c, single := designRun(t, mode, "--file", r.Scope.File)
			var one systemdesign.Report
			if err := json.Unmarshal(single, &one); err != nil {
				t.Fatal(err)
			}
			x, _ := json.Marshal(r)
			y, _ := json.Marshal(one)
			if c != 0 || !bytes.Equal(x, y) {
				t.Fatalf("single differs from all: %s / %s", x, y)
			}
		}
	}
	// Child ownership applies even when its design is invalid, and batches continue.
	write(t, filepath.Join(child, "system-design.yaml"), "invalid: true\n")
	code, b = designRun(t, "check", "--file", filepath.Join(root, "system-design.yaml"))
	if code != 0 {
		t.Fatalf("parent inspected child content: %d %s", code, b)
	}
	code, b = designRun(t, "check", "--root", repo, "--all")
	var batch designBatch
	_ = json.Unmarshal(b, &batch)
	if code != 1 || len(batch.Results) != 3 {
		t.Fatalf("batch stopped early: %d %s", code, b)
	}
	// Required files cannot reach into child ownership.
	write(t, filepath.Join(root, "system-design.yaml"), strings.Replace(scopedDesign, "[core.go]", "[core.go, child/src/domain/core/core.go]", 1))
	code, b = designRun(t, "validate", "--file", filepath.Join(root, "system-design.yaml"))
	if code != 1 || !bytes.Contains(b, []byte("scope.overlap")) {
		t.Fatalf("%d %s", code, b)
	}
}

func TestSystemDesignCrossScopeImports(t *testing.T) {
	t.Setenv("GOWORK", "off")
	repo := t.TempDir()
	write(t, filepath.Join(repo, ".git"), "gitdir: irrelevant\n")
	root := filepath.Join(repo, "a")
	scopeProject(t, root, "example.com/a")
	child := filepath.Join(root, "src/domain/core/child")
	scopeProject(t, child, "")
	sibling := filepath.Join(repo, "b")
	scopeProject(t, sibling, "example.com/b")
	for _, tc := range []struct{ root, imp string }{
		{root, "example.com/a/src/domain/core/child/src/domain/core"},
		{child, "example.com/a/src/domain/core"},
		{root, "example.com/b/src/domain/core"},
	} {
		p := filepath.Join(tc.root, "src/domain/core/core.go")
		write(t, p, "package core\nimport _ \""+tc.imp+"\"\n")
		code, b := designRun(t, "check", "--file", filepath.Join(tc.root, "system-design.yaml"))
		if code != 1 || !bytes.Contains(b, []byte("dependency.cross-scope")) {
			t.Fatalf("%s: %d %s", tc.imp, code, b)
		}
		write(t, p, "package core\n")
	}
}

func TestSystemDesignDiscoveryAndArguments(t *testing.T) {
	root := t.TempDir()
	for _, mode := range []string{"list", "validate", "check"} {
		args := []string{mode, "--root", root}
		if mode != "list" {
			args = append(args, "--all")
		}
		code, b := designRun(t, args...)
		if code != 2 || !bytes.Contains(b, []byte("no system-design.yaml")) {
			t.Fatalf("%d %s", code, b)
		}
	}
	scopeProject(t, filepath.Join(root, "app"), "example.com/app")
	for _, excluded := range []string{".git", "node_modules", "vendor"} {
		scopeProject(t, filepath.Join(root, excluded), "example.com/ignored")
	}
	code, b := designRun(t, "list", "--root", root)
	var batch designBatch
	_ = json.Unmarshal(b, &batch)
	if code != 0 || len(batch.Scopes) != 3 {
		t.Fatalf("%d %s", code, b)
	}
	for _, args := range [][]string{
		{"check", "--all", "--file", "system-design.yaml"},
		{"render", "--all"}, {"list", "--all"}, {"validate", "--file", "other.yaml"},
		{"check", "--all", "--profile", "layers"},
	} {
		if code, b := designRun(t, args...); code != 2 {
			t.Fatalf("%v: %d %s", args, code, b)
		}
	}
	// Discovery does not follow directory links or accept linked design files.
	external := t.TempDir()
	scopeProject(t, external, "example.com/external")
	if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	code, b = designRun(t, "list", "--root", root)
	_ = json.Unmarshal(b, &batch)
	if code != 0 || len(batch.Scopes) != 3 {
		t.Fatalf("followed directory symlink: %d %s", code, b)
	}
	if err := os.Symlink(filepath.Join(external, "system-design.yaml"), filepath.Join(root, "system-design.yaml")); err != nil {
		t.Fatal(err)
	}
	code, b = designRun(t, "validate", "--file", filepath.Join(root, "system-design.yaml"))
	if code != 2 {
		t.Fatalf("followed file symlink: %d %s", code, b)
	}
}

func TestSystemDesignBatchUnavailableContinues(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	scopeProject(t, filepath.Join(root, "a"), "example.com/a")
	scopeProject(t, filepath.Join(root, "b"), "") // Go source without module: unavailable import analysis
	scopeProject(t, filepath.Join(root, "c"), "example.com/c")
	code, b := designRun(t, "check", "--root", root, "--all")
	var batch designBatch
	_ = json.Unmarshal(b, &batch)
	if code != 2 || len(batch.Results) != 3 || batch.Results[2].Status != "passed" {
		t.Fatalf("%d %s", code, b)
	}
}

func TestSystemDesignStructureCheckDoesNotRequireGo(t *testing.T) {
	root := t.TempDir()
	design := strings.Replace(scopedDesign, "[core.go]", "[index.ts]", 1)
	write(t, filepath.Join(root, "system-design.yaml"), design)
	if err := os.MkdirAll(filepath.Join(root, "src/domain/core"), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "src/domain/core/index.ts"), "export {};\n")
	code, b := designRun(t, "check", "--file", filepath.Join(root, "system-design.yaml"))
	var report systemdesign.Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if code != 0 || report.Status != "passed" || len(report.Diagnostics) != 0 {
		t.Fatalf("%d %s", code, b)
	}
	found := false
	for _, item := range report.Coverage {
		if strings.Contains(item, "TypeScript files parsed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("TypeScript adapter did not run without Go: %s", b)
	}
}

func TestSystemDesignParentDeclarationsAndIndependentChild(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	scopeProject(t, root, "example.com/root")
	child := filepath.Join(root, "src/domain/core/child")
	scopeProject(t, child, "example.com/root/child")
	// The longer child module prefix wins over its parent's module prefix.
	write(t, filepath.Join(child, "src/domain/core/core.go"), "package core\nimport _ \"example.com/root/child/src/domain/core\"\n")
	code, b := designRun(t, "check", "--file", filepath.Join(child, "system-design.yaml"))
	if code != 0 {
		t.Fatalf("own child-module import rejected: %d %s", code, b)
	}
	for _, boundary := range []string{"src", "src/domain", "src/domain/core"} {
		file := filepath.Join(root, filepath.FromSlash(boundary), "system-design.yaml")
		write(t, file, scopedDesign)
		code, b = designRun(t, "validate", "--file", filepath.Join(root, "system-design.yaml"))
		if code != 1 || !bytes.Contains(b, []byte("scope.overlap")) {
			t.Fatalf("%s: %d %s", boundary, code, b)
		}
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	// --root is not the implementation scope when --file is absolute.
	code, b = designRun(t, "check", "--root", t.TempDir(), "--file", filepath.Join(root, "system-design.yaml"))
	if code != 0 {
		t.Fatalf("root overrode file directory: %d %s", code, b)
	}
}

func TestSystemDesignInvalidChildFileDoesNotStopBatch(t *testing.T) {
	root := t.TempDir()
	scopeProject(t, filepath.Join(root, "a"), "example.com/a")
	scopeProject(t, filepath.Join(root, "b"), "example.com/b")
	// A directory with the reserved filename is a per-design unavailable result.
	if err := os.Mkdir(filepath.Join(root, "system-design.yaml"), 0755); err != nil {
		t.Fatal(err)
	}
	code, b := designRun(t, "validate", "--root", root, "--all")
	var batch designBatch
	if err := json.Unmarshal(b, &batch); err != nil {
		t.Fatal(err)
	}
	if code != 2 || len(batch.Results) != 3 {
		t.Fatalf("%d %s", code, b)
	}
	passed := 0
	for _, r := range batch.Results {
		if r.Status == "passed" {
			passed++
		}
	}
	if passed != 2 {
		t.Fatalf("valid designs were skipped: %s", b)
	}
}
