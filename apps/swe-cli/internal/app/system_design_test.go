package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitectureCommands(t *testing.T) {
	root := "../systemdesign/testdata/project"
	for _, mode := range []string{"validate", "check", "render"} {
		t.Run(mode, func(t *testing.T) {
			var out bytes.Buffer
			code := Run([]string{"system-design", mode, "--root", root, "--file", "system-design.yaml"}, nil, &out)
			if code != 0 {
				t.Fatalf("%d %s", code, out.String())
			}
			if mode == "render" {
				if !strings.HasPrefix(out.String(), "<!doctype html>") {
					t.Fatal(out.String())
				}
			} else {
				var result map[string]any
				if e := json.Unmarshal(out.Bytes(), &result); e != nil || result["status"] != "passed" {
					t.Fatal(out.String(), e)
				}
			}
		})
	}
}
func TestArchitectureInvalidNeverRenders(t *testing.T) {
	p := filepath.Join(t.TempDir(), "system-design.yaml")
	if e := os.WriteFile(p, []byte("schema_version: 1\n"), 0644); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if code := Run([]string{"system-design", "render", "--file", p}, nil, &out); code != 1 || strings.Contains(out.String(), "<!doctype") {
		t.Fatalf("%d %s", code, out.String())
	}
}
func TestArchitectureInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"unknown"}, {"check"}, {"validate", "--other", "a"}, {"check", "--file", "/missing/system-design.yaml"}} {
		var out bytes.Buffer
		if code := SystemDesign(args, &out); code != 2 {
			t.Fatalf("%v: %d %s", args, code, out.String())
		}
	}
}

func TestArchitectureWorkspaceReturnsUnavailable(t *testing.T) {
	t.Setenv("GOWORK", filepath.Join(t.TempDir(), "external.work"))
	var out bytes.Buffer
	code := Run([]string{"system-design", "check", "--root", "../systemdesign/testdata/project", "--file", "system-design.yaml"}, nil, &out)
	var report struct {
		Status string `json:"status"`
	}
	if e := json.Unmarshal(out.Bytes(), &report); e != nil {
		t.Fatal(e)
	}
	if code != 2 || report.Status != "unavailable" {
		t.Fatalf("%d %s", code, out.String())
	}
}

func TestArchitectureConflictingFilesCannotRender(t *testing.T) {
	b, e := os.ReadFile("../systemdesign/testdata/project/system-design.yaml")
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "system-design.yaml")
	write(t, file, strings.Replace(string(b), "[service.go]", "[service.go, service.go/child.go]", 1))
	for _, mode := range []string{"validate", "render"} {
		var out bytes.Buffer
		code := Run([]string{"system-design", mode, "--file", file}, nil, &out)
		var report struct {
			Status string `json:"status"`
		}
		if e := json.Unmarshal(out.Bytes(), &report); e != nil {
			t.Fatal(e)
		}
		if code != 1 || report.Status != "violations" {
			t.Fatalf("%s: %d %s", mode, code, out.String())
		}
	}
}

// Invoke the actual command dispatcher in fresh processes, avoiding a shared
// template instance, caches, or map iteration seed masking nondeterminism.
func TestRenderDeterminismProcess(t *testing.T) {
	if file := os.Getenv("REED_TEST_RENDER_INPUT"); file != "" {
		os.Exit(Run([]string{"system-design", "render", "--file", file}, nil, os.Stdout))
	}
}

func TestArchitectureRenderDeterministicAcrossProcesses(t *testing.T) {
	data, err := os.ReadFile("../systemdesign/testdata/project/system-design.yaml")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	render := func(file string) []byte {
		cmd := exec.Command(binary, "-test.run=^TestRenderDeterminismProcess$")
		cmd.Env = append(os.Environ(), "REED_TEST_RENDER_INPUT="+file)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("render failed: %v\n%s", err, out)
		}
		if !bytes.HasPrefix(out, []byte("<!doctype html>")) {
			t.Fatalf("not HTML: %s", out)
		}
		return out
	}
	first := filepath.Join(t.TempDir(), "system-design.yaml")
	if err = os.WriteFile(first, data, 0600); err != nil {
		t.Fatal(err)
	}
	expected := render(first)
	for i := 0; i < 3; i++ {
		if actual := render(first); !bytes.Equal(expected, actual) {
			t.Fatal("same YAML produced different HTML across processes")
		}
	}
	// The checkout location must not leak into the generated page.
	relocated := filepath.Join(t.TempDir(), "system-design.yaml")
	if err = os.WriteFile(relocated, data, 0600); err != nil {
		t.Fatal(err)
	}
	if actual := render(relocated); !bytes.Equal(expected, actual) {
		t.Fatal("output depends on absolute YAML location")
	}
	changed := bytes.Replace(data, []byte("订单处理"), []byte("确定性回归测试的新职责"), 1)
	if bytes.Equal(changed, data) {
		t.Fatal("fixture responsibility missing")
	}
	if err = os.WriteFile(first, changed, 0600); err != nil {
		t.Fatal(err)
	}
	actual := render(first)
	if bytes.Equal(expected, actual) || !bytes.Contains(actual, []byte("确定性回归测试的新职责")) {
		t.Fatal("render ignored changed YAML")
	}
}
