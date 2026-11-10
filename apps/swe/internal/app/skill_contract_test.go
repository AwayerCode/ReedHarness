package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Execute the published examples so documentation cannot silently drift from
// accepted arguments and output contracts. Templates intentionally fail content
// validation, but must produce a normal check result rather than a request error.
func TestSkillCommandContracts(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs/app-design.md"))
	if err != nil {
		t.Fatal(err)
	}
	field := regexp.MustCompile("结果数组为 `([^`]+)`").FindSubmatch(doc)
	if len(field) != 2 {
		t.Fatal("missing documented check result field")
	}
	fixture, err := filepath.Abs("../systemdesign/testdata/project")
	if err != nil {
		t.Fatal(err)
	}
	repl := strings.NewReplacer(
		"<项目根目录>", root,
		"<模型路径>", "skills/swe-business-model/assets/business-model.md",
		"<Markdown路径>", "skills/swe-system-design/assets/system-design.md",
		"<协议路径>", "skills/swe-system-design/assets/interface-protocol.md",
		"<测试设计路径>", "skills/swe-testing/assets/test-design.md",
		"<相对路径>", "apps/swe",
		"<固定版本>", Version,
	)
	count := 0
	for _, line := range strings.Split(string(doc), "\n") {
		if !strings.HasPrefix(line, "<CLI> ") || strings.HasPrefix(line, "<CLI> scope ") {
			continue
		}
		count++
		t.Run(strings.TrimPrefix(line, "<CLI> "), func(t *testing.T) {
			command := strings.TrimPrefix(line, "<CLI> ")
			if strings.HasPrefix(command, "system-design ") {
				command = strings.ReplaceAll(command, "<项目根目录>", fixture)
				command = strings.ReplaceAll(command, "<YAML路径>", "system-design.yaml")
			}
			args := strings.Fields(repl.Replace(command))
			if len(args) > 1 && args[0] == "system-design" && args[1] == "preview" {
				testPreviewCommand(t, args)
				return
			}
			var out bytes.Buffer
			code := Run(args, nil, &out)
			if code != 0 && code != 1 {
				t.Fatalf("example unavailable: %d %s", code, &out)
			}
			if len(args) > 1 && args[0] == "system-design" && args[1] == "render" {
				if code != 0 || !strings.HasPrefix(out.String(), "<!doctype html>") {
					t.Fatal(out.String())
				}
				return
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if args[0] == "version" {
				if len(result["version"]) == 0 {
					t.Fatal(out.String())
				}
				return
			}
			if args[0] == "system-design" {
				if string(result["format"]) == `"swe-system-design-batch-v1"` {
					assertBatchResultContract(t, out.Bytes(), args[1])
				}
				if (string(result["format"]) != `"swe-system-design-v1"` && string(result["format"]) != `"swe-system-design-batch-v1"`) || string(result["status"]) != `"passed"` {
					t.Fatal(out.String())
				}
				return
			}
			var checks []map[string]json.RawMessage
			if err := json.Unmarshal(result[string(field[1])], &checks); err != nil || len(checks) == 0 {
				t.Fatalf("documented field missing: %s", &out)
			}
			for _, check := range checks {
				for _, key := range []string{"status", "diagnostics", "evidence", "coverage"} {
					if _, ok := check[key]; !ok {
						t.Fatalf("missing %s: %s", key, &out)
					}
				}
			}
		})
	}
	if count == 0 {
		t.Fatal("no CLI examples exercised")
	}
}

func TestArchitectureUnavailableContract(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{}, {"unknown"}, {"check"}, {"validate", "--unknown"},
		{"check", "--file", "unused", "--profile", "layers"},
		{"validate", "--file", filepath.Join(root, "missing.yaml")},
		{"render", "--file", root}, // opening succeeds on Unix, reading a directory fails
	} {
		var out bytes.Buffer
		code := Run(append([]string{"system-design"}, args...), nil, &out)
		var r struct {
			Format      string `json:"format"`
			Status      string `json:"status"`
			Digest      string `json:"design_sha256"`
			Diagnostics []any  `json:"diagnostics"`
		}
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if code != 2 || r.Format != "swe-system-design-v1" || r.Status != "unavailable" || r.Digest != "" || len(r.Diagnostics) == 0 {
			t.Fatalf("%v: %d %s", args, code, &out)
		}
	}
}

func assertBatchResultContract(t *testing.T, data []byte, mode string) designBatch {
	t.Helper()
	var batch designBatch
	if err := json.Unmarshal(data, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.Format != "swe-system-design-batch-v1" || batch.Mode != mode {
		t.Fatalf("unexpected batch: %s", data)
	}
	if batch.Scopes == nil || batch.Results == nil {
		t.Fatalf("arrays must not be absent or null: %s", data)
	}
	if mode == "list" {
		if len(batch.Results) != 0 {
			t.Fatalf("list must not include validation reports: %s", data)
		}
		return batch
	}
	if len(batch.Results) != len(batch.Scopes) {
		t.Fatalf("scope/report count differs: %s", data)
	}
	seen := map[string]bool{}
	for i, s := range batch.Scopes {
		if seen[s.File] {
			t.Fatalf("duplicate scope: %s", s.File)
		}
		seen[s.File] = true
		r := batch.Results[i]
		if r.Scope == nil || !reflect.DeepEqual(s, *r.Scope) || r.Mode != mode || r.Format != "swe-system-design-v1" {
			t.Fatalf("scope/report identity or order differs at %d: %s", i, data)
		}
	}
	return batch
}

func TestListDoesNotValidateAndBatchIncludesFailures(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	for _, name := range []string{"a-invalid", "b-unavailable", "c-valid"} {
		scopeProject(t, filepath.Join(root, name), "example.com/"+name)
	}
	write(t, filepath.Join(root, "a-invalid/system-design.yaml"), "invalid: [\n")
	file := filepath.Join(root, "b-unavailable/system-design.yaml")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"list", "validate", "check"} {
		args := []string{mode, "--root", root}
		if mode != "list" {
			args = append(args, "--all")
		}
		code, data := designRun(t, args...)
		if code != 2 {
			t.Fatalf("expected unavailable entry: %d %s", code, data)
		}
		batch := assertBatchResultContract(t, data, mode)
		if len(batch.Scopes) != 3 {
			t.Fatalf("missing scope: %s", data)
		}
		if mode != "list" {
			for i, want := range []string{"violations", "unavailable", "passed"} {
				if batch.Results[i].Status != want {
					t.Fatalf("unexpected report %d: %s", i, data)
				}
			}
		}
	}
	// With only regular files, syntactically invalid YAML does not fail list.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	write(t, file, scopedDesign)
	code, data := designRun(t, "list", "--root", root)
	if code != 0 {
		t.Fatalf("list parsed invalid YAML: %d %s", code, data)
	}
	assertBatchResultContract(t, data, "list")
}

func TestExplicitSymlinkDirectoryResolvesToRealScope(t *testing.T) {
	t.Setenv("GOWORK", "off")
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scopeProject(t, real, "example.com/real")
	entry := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, entry); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	code, data := designRun(t, "list", "--root", entry)
	if code != 0 {
		t.Fatalf("%d %s", code, data)
	}
	batch := assertBatchResultContract(t, data, "list")
	if len(batch.Scopes) != 1 || batch.Scopes[0].Root != real || batch.Scopes[0].File != filepath.Join(real, "system-design.yaml") {
		t.Fatalf("not canonical: %s", data)
	}
	for _, mode := range []string{"validate", "check"} {
		code, a := designRun(t, mode, "--file", filepath.Join(entry, "system-design.yaml"))
		direct, b := designRun(t, mode, "--file", filepath.Join(real, "system-design.yaml"))
		if code != 0 || direct != 0 || !bytes.Equal(a, b) {
			t.Fatalf("explicit alias differs: %d %s / %d %s", code, a, direct, b)
		}
	}
}
