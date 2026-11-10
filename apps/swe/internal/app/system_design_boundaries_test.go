package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AwayerCode/ReedHarness/apps/swe/internal/systemdesign"
)

func assertDesignBatchMatchesSingles(t *testing.T, mode, root string, wantCode int) designBatch {
	t.Helper()
	code, b := designRun(t, mode, "--root", root, "--all")
	var batch designBatch
	if err := json.Unmarshal(b, &batch); err != nil {
		t.Fatal(err)
	}
	if code != wantCode {
		t.Fatalf("%d %s", code, b)
	}
	for _, r := range batch.Results {
		singleCode, single := designRun(t, mode, "--file", r.Scope.File)
		var one systemdesign.Report
		if err := json.Unmarshal(single, &one); err != nil {
			t.Fatal(err)
		}
		expectedCode := 0
		if r.Status == "violations" {
			expectedCode = 1
		}
		if r.Status == "unavailable" {
			expectedCode = 2
		}
		a, _ := json.Marshal(r)
		b, _ := json.Marshal(one)
		if singleCode != expectedCode || !bytes.Equal(a, b) {
			t.Fatalf("single/all differ: %d %s / %s", singleCode, a, b)
		}
	}
	return batch
}

func TestHiddenAndDependencyDirectoriesOwnChildScopes(t *testing.T) {
	t.Setenv("GOWORK", "off")
	for _, dir := range []string{".child", "vendor", "node_modules"} {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			scopeProject(t, root, "example.com/root")
			child := filepath.Join(root, "src/domain/core", dir)
			scopeProject(t, child, "")
			batch := assertDesignBatchMatchesSingles(t, "check", root, 0)
			if len(batch.Results) != 2 {
				t.Fatalf("child not discovered: %+v", batch)
			}
			// Parent declarations and imports must not cross the formerly excluded boundary.
			write(t, filepath.Join(root, "system-design.yaml"), strings.Replace(scopedDesign, "[core.go]", "[core.go, "+dir+"/src/domain/core/core.go]", 1))
			assertDesignBatchMatchesSingles(t, "validate", root, 1)
			write(t, filepath.Join(root, "system-design.yaml"), scopedDesign)
			write(t, filepath.Join(root, "src/domain/core/core.go"), "package core\nimport _ \"example.com/root/src/domain/core/"+dir+"/src/domain/core\"\n")
			batch = assertDesignBatchMatchesSingles(t, "check", root, 1)
			found := false
			for _, r := range batch.Results {
				for _, d := range r.Diagnostics {
					if d.Rule == "dependency.cross-scope" {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("missing cross-scope diagnostic")
			}
		})
	}
	// Git metadata cannot be a scope even when explicitly selected.
	root := t.TempDir()
	scopeProject(t, filepath.Join(root, ".git"), "example.com/meta")
	if code, b := designRun(t, "validate", "--file", filepath.Join(root, ".git/system-design.yaml")); code != 2 {
		t.Fatalf("%d %s", code, b)
	}
}

func TestInvalidSiblingModuleCannotBecomeExternal(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	write(t, filepath.Join(root, ".git"), "gitdir: unused\n")
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	c := filepath.Join(root, "c")
	scopeProject(t, a, "example.com/a")
	scopeProject(t, b, "example.com/b")
	scopeProject(t, c, "example.com/c")
	write(t, filepath.Join(c, "src/domain/core/core.go"), "package core\nimport _ \"fmt\"\n")
	if err := os.Remove(filepath.Join(b, "go.mod")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "module-metadata")
	write(t, target, "module example.com/b\n")
	if err := os.Symlink(target, filepath.Join(b, "go.mod")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	write(t, filepath.Join(a, "src/domain/core/core.go"), "package core\nimport _ \"example.com/b/src/domain/core\"\n")
	batch := assertDesignBatchMatchesSingles(t, "check", root, 2)
	if len(batch.Results) != 3 || batch.Results[0].Status != "unavailable" || batch.Results[2].Status != "passed" {
		t.Fatalf("%+v", batch)
	}
	if len(batch.Results[0].Unverified) == 0 {
		t.Fatal("metadata failure not recorded")
	}
}

func TestUnreadableBranchBatchContinues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions; deterministic walk-error coverage is in systemdesign tests")
	}
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	write(t, filepath.Join(root, ".git"), "gitdir: unused\n")
	a := filepath.Join(root, "a")
	c := filepath.Join(root, "c")
	scopeProject(t, a, "example.com/a")
	scopeProject(t, c, "example.com/c")
	blocked := filepath.Join(a, "blocked")
	if err := os.Mkdir(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0700) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("current user bypasses directory permissions; deterministic test covers this path")
	}
	batch := assertDesignBatchMatchesSingles(t, "check", root, 2)
	if len(batch.Issues) != 1 || len(batch.Results) != 2 || batch.Results[0].Status != "unavailable" || batch.Results[1].Status != "passed" {
		t.Fatalf("%+v", batch)
	}
}

func TestInvalidDesignEntrySingleAndBatchMatch(t *testing.T) {
	root := t.TempDir()
	scopeProject(t, filepath.Join(root, "valid"), "example.com/valid")
	if err := os.Mkdir(filepath.Join(root, "system-design.yaml"), 0755); err != nil {
		t.Fatal(err)
	}
	assertDesignBatchMatchesSingles(t, "validate", root, 2)
}

func TestUnreadableBranchOutsideScopesStillReportsFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions; deterministic tests cover walk errors")
	}
	root := t.TempDir()
	scopeProject(t, filepath.Join(root, "valid"), "example.com/valid")
	blocked := filepath.Join(root, "unknown")
	if err := os.Mkdir(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0700) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("current user bypasses directory permissions")
	}
	batch := assertDesignBatchMatchesSingles(t, "validate", root, 2)
	if len(batch.Issues) != 1 || len(batch.Results) != 1 || batch.Results[0].Status != "passed" {
		t.Fatalf("%+v", batch)
	}
}
