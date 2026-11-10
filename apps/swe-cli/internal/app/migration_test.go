package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMigrationCases(t *testing.T) {
	data, e := os.ReadFile("testdata/migration.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name    string
		Command string
		Files   map[string]string
		Args    []string
		Code    int
	}
	if e = json.Unmarshal(data, &cases); e != nil {
		t.Fatal(e)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			root, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			for name, text := range c.Files {
				p := filepath.Join(root, name)
				if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
					t.Fatal(e)
				}
				write(t, p, text)
			}
			args := append([]string{c.Command, "--root", root}, c.Args...)
			var out bytes.Buffer
			code := Run(args, nil, &out)
			if code != c.Code {
				t.Fatalf("got %d want %d: %s", code, c.Code, out.String())
			}
		})
	}
}

func TestArtifactsExplicitScenarios(t *testing.T) {
	root, _ := fixture(t)
	// Build a conforming test-design document, with explicit risk/scenario relations.
	data, e := os.ReadFile(filepath.Join(repository(t), "apps/swe-cli/resources/artifact-schemas/test-design.json"))
	if e != nil {
		t.Fatal(e)
	}
	var schema struct {
		Title    string
		Sections []struct {
			Heading string
			Columns []string
		}
	}
	if e = json.Unmarshal(data, &schema); e != nil {
		t.Fatal(e)
	}
	text := "# " + schema.Title + "\n\n"
	for _, section := range schema.Sections {
		text += "## " + section.Heading + "\n\n"
		switch section.Heading {
		case "文档信息":
			text += "| 字段 | 值 |\n| --- | --- |\n| 类型 | test-design |\n| 任务 | 测试 |\n| 范围 | 范围 |\n| 依据 | 依据 |\n| 状态 | 草案 |\n"
		default:
			if slices.Contains(section.Columns, "风险标识") && slices.Contains(section.Columns, "场景") {
				text += "| " + strings.Join(section.Columns, " | ") + " |\n|"
				for range section.Columns {
					text += " --- |"
				}
				text += "\n|"
				for _, col := range section.Columns {
					v := "内容"
					if col == "风险标识" {
						v = "R1"
					}
					if col == "场景" {
						v = "场景:S1"
					}
					text += " " + v + " |"
				}
				text += "\n"
			} else {
				text += "不适用：当前范围无需。\n"
			}
		}
		text += "\n"
	}
	text = strings.TrimRight(text, "\n") + "\n\n### 场景:S1\n\n验证内容\n"
	for _, tc := range []struct {
		name, text string
		code       int
	}{{"valid", text, 0}, {"dangling", strings.Replace(text, "场景:S1", "场景:missing", 1), 1}, {"invalid-token", strings.Replace(text, "场景:S1", "场景:S1/invalid", 1), 1}, {"duplicate", text + "\n### 场景:S1\n\n重复\n", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			write(t, filepath.Join(root, "test.md"), tc.text)
			var out bytes.Buffer
			if code := Run([]string{"artifacts", "--root", root, "--kind", "test", "--file", "test.md"}, nil, &out); code != tc.code {
				t.Fatal(code, out.String())
			}
		})
	}
}
