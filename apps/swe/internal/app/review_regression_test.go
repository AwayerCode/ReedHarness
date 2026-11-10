package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMalformedAdditionalUITableReturnsDiagnostics(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"business-model", "system-design", "test-design", "interface-protocol", "interaction-standards"} {
		b, err := os.ReadFile(filepath.Join("testdata/documents", name+".md"))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(root, name+".md"), string(b))
	}
	file := filepath.Join(root, "interaction-standards.md")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	write(t, file, string(original)+"\n| 用户操作 | 公共能力 | 可见状态 | 反馈 | 后续动作 |\n|---|---|---|---|---|\n| broken |\n")
	var out bytes.Buffer
	code := Run([]string{"artifacts", "--root", root, "--kind", "ui", "--file", "interaction-standards.md", "--protocol", "interface-protocol.md"}, nil, &out)
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("no structured report: %v: %s", err, &out)
	}
	if code != 1 || !strings.Contains(out.String(), "用户操作表格列数错误") {
		t.Fatalf("%d: %s", code, &out)
	}
}

func TestCommandHelp(t *testing.T) {
	for _, path := range [][]string{
		{}, {"check"}, {"documents"}, {"design"}, {"artifacts"}, {"structure"},
		{"scope"}, {"scope", "snapshot"}, {"scope", "check"}, {"scope", "readonly"},
		{"system-design"}, {"system-design", "list"}, {"system-design", "validate"},
		{"system-design", "check"}, {"system-design", "render"},
	} {
		for _, flag := range []string{"-h", "--help"} {
			args := append(append([]string{}, path...), flag)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				var out bytes.Buffer
				if code := Run(args, nil, &out); code != 0 || !strings.Contains(out.String(), "swe") || json.Valid(out.Bytes()) {
					t.Fatalf("%d: %s", code, &out)
				}
			})
		}
	}
	var out bytes.Buffer
	if code := Run([]string{"help", "documents"}, nil, &out); code != 0 || !strings.Contains(out.String(), "-kind") {
		t.Fatalf("%d: %s", code, &out)
	}
	if strings.Contains(out.String(), "-scope") {
		t.Fatal("unrelated command flags in help")
	}
	out.Reset()
	if code := Run([]string{"documents", "--unknown"}, nil, &out); code != 2 || !json.Valid(out.Bytes()) {
		t.Fatalf("%d: %s", code, &out)
	}
}
