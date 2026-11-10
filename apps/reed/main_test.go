package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStandaloneBinary(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "独立 空格")
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := "reed"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	fixture, err := os.ReadFile("internal/app/testdata/documents/business-model.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "model.md"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	req := map[string]any{"protocolVersion": 2, "taskId": "standalone", "round": 1, "mode": "review", "projectRoot": dir, "checks": []any{map[string]any{"checkId": "documents", "required": true, "input": map[string]any{"kind": "business-model", "file": "model.md"}}}}
	b, _ := json.Marshal(req)
	request := filepath.Join(dir, "request.json")
	if err = os.WriteFile(request, b, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "check", "--request", request)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + filepath.Join(root, "no-runtimes"), "SystemRoot=" + os.Getenv("SystemRoot")}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("standalone: %v %s", err, out)
	}
	var result struct{ Summary struct{ Status string } }
	if err = json.Unmarshal(out, &result); err != nil || result.Summary.Status != "passed" {
		t.Fatal(string(out), err)
	}
	// The standalone executable must read its Skills without repository or PATH resources.
	for _, resource := range []string{"SKILL.md", "references/cli.md"} {
		want, err := os.ReadFile(filepath.Join("../../skills/swe-testing", resource))
		if err != nil {
			t.Fatal(err)
		}
		read := exec.Command(binary, "skills", "read", "swe-testing", resource)
		read.Dir = root
		read.Env = cmd.Env
		got, err := read.CombinedOutput()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("standalone Skill %s: %v %s", resource, err, got)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatal("unexpected Review writes", err, entries)
	}
}
