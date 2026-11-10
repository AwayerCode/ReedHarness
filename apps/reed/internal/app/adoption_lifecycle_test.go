package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AwayerCode/ReedHarness/apps/reed/internal/workspace"
)

// A real subprocess exercises the retained executable and its binding after the
// original installed package is replaced or deleted, not just Run in-process.
func TestAdoptionCLIProcess(t *testing.T) {
	if os.Getenv("REED_ADOPTION_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(RunInstalled(os.Args[i+1:], nil, os.Stdout))
		}
	}
	os.Exit(99)
}

func adoptionRepo(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return root
}

func adoptionPackage(t *testing.T, root string) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	name := "reed"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(root, "bin", name)
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, data, 0700); err != nil {
		t.Fatal(err)
	}
	binding, err := json.Marshal(map[string]any{"schema_version": 1, "name": "reed", "version": Version, "assets": map[string]any{runtime.GOOS + "/" + runtime.GOARCH: map[string]string{"sha256": fmt.Sprintf("%x", sha256.Sum256(data))}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runtime.json"), binding, 0600); err != nil {
		t.Fatal(err)
	}
	return bin
}

func adoptionCLI(t *testing.T, bin string, want int, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"-test.run=^TestAdoptionCLIProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "REED_ADOPTION_TEST_PROCESS=1")
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if code != want {
		t.Fatalf("%v: code %d want %d: %s", args, code, want, out)
	}
}

func TestAdoptionRetainedToolAfterUpdateAndExit(t *testing.T) {
	for _, operation := range []string{"update", "exit"} {
		t.Run(operation, func(t *testing.T) {
			root := adoptionRepo(t)
			installed := filepath.Join(root, ".tools", "reed")
			original := adoptionPackage(t, installed)
			retained := filepath.Join(t.TempDir(), "swe")
			if err := os.CopyFS(retained, os.DirFS(installed)); err != nil {
				t.Fatal(err)
			}
			tool := filepath.Join(retained, "bin", filepath.Base(original))
			adoptionCLI(t, tool, 0, "version")
			baseline := filepath.Join(t.TempDir(), "scope.json")
			adoptionCLI(t, tool, 0, "scope", "snapshot", "--root", root, "--baseline", baseline, "--allow-dir", ".tools/reed")
			if operation == "update" {
				candidate := filepath.Join(t.TempDir(), "swe")
				next := adoptionPackage(t, candidate)
				// Reject an incomplete/mismatched candidate before replacing the package.
				binding := filepath.Join(candidate, "runtime.json")
				valid, err := os.ReadFile(binding)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(binding, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				adoptionCLI(t, next, 2, "version")
				adoptionCLI(t, original, 0, "version")
				if err := os.WriteFile(binding, valid, 0600); err != nil {
					t.Fatal(err)
				}
				adoptionCLI(t, next, 0, "version")
				if err := os.RemoveAll(installed); err != nil {
					t.Fatal(err)
				}
				if err := os.CopyFS(installed, os.DirFS(candidate)); err != nil {
					t.Fatal(err)
				}
			}
			if operation == "exit" {
				if err := os.RemoveAll(installed); err != nil {
					t.Fatal(err)
				}
			}
			adoptionCLI(t, tool, 0, "scope", "check", "--root", root, "--baseline", baseline)
			// The retained tool must still detect unrelated changes, not merely execute.
			if err := os.WriteFile(filepath.Join(root, "unrelated"), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			adoptionCLI(t, tool, 1, "scope", "check", "--root", root, "--baseline", baseline)
		})
	}
}

func TestAdoptionMixedDirectoryMigration(t *testing.T) {
	root := adoptionRepo(t)
	old := filepath.Join(root, "old")
	next := filepath.Join(root, "next")
	for _, dir := range []string{old, next} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"service.go", "service_test.go"} {
		if err := os.WriteFile(filepath.Join(old, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	baseline := filepath.Join(t.TempDir(), "production.json")
	if err := workspace.Snapshot(root, baseline, []string{"old/service.go", "next/service.go"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(old, "service.go"), filepath.Join(next, "service.go")); err != nil {
		t.Fatal(err)
	}
	_, unexpected, err := workspace.Check(root, baseline)
	if err != nil || len(unexpected) != 0 {
		t.Fatal(unexpected, err)
	}
	data, err := os.ReadFile(filepath.Join(old, "service_test.go"))
	if err != nil || !bytes.Equal(data, []byte("service_test.go")) {
		t.Fatal("test moved with production", err)
	}
	if err := os.Rename(filepath.Join(old, "service_test.go"), filepath.Join(next, "service_test.go")); err != nil {
		t.Fatal(err)
	}
	_, unexpected, err = workspace.Check(root, baseline)
	if err != nil || len(unexpected) != 2 {
		t.Fatalf("test migration must report both paths: %v %v", unexpected, err)
	}
}
