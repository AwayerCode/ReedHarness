package systemdesign

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	e := filepath.WalkDir("testdata/project", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel("testdata/project", p)
		dest := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(dest, b, 0644)
	})
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(root, "system-design.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	return root, b
}
func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(body), 0644); e != nil {
		t.Fatal(e)
	}
}
func has(r Report, rule string) bool {
	for _, d := range r.Diagnostics {
		if d.Rule == rule {
			return true
		}
	}
	return false
}
func TestSchemaMatchesCanonical(t *testing.T) {
	b, e := os.ReadFile("../../resources/system-design/schema.json")
	if e != nil || string(b) != SchemaJSON {
		t.Fatal("run go generate", e)
	}
}
func TestDesignRejections(t *testing.T) {
	_, b := fixture(t)
	s := string(b)
	for _, tc := range []struct{ name, body, rule string }{
		{"unknown", s + "unknown: true\n", "schema"},
		{"missing", strings.Replace(s, "schema_version: 1\n", "", 1), "schema"},
		{"version", strings.Replace(s, "schema_version: 1", "schema_version: 2", 1), "schema"},
		{"type", strings.Replace(s, "schema_version: 1", "schema_version: '1'", 1), "schema"},
		{"duplicate key", s + "source_root: other\n", "yaml.parse"},
		{"multiple documents", s + "---\na: b\n", "yaml.documents"},
		{"alias", strings.Replace(s, "source_root: src", "source_root: &source src", 1) + "other: *source\n", "yaml.parse"},
		{"traversal", strings.Replace(s, "source_root: src", "source_root: ../src", 1), "path.source"},
		{"required escape", strings.Replace(s, "[service.go]", "[../service.go]", 1), "path.file"},
		{"path collision", strings.Replace(s, "directory: payments", "directory: orders", 1), "path.overlap"},
		{"layer case collision", strings.Replace(s, "directory: infrastructure", "directory: APPLICATION", 1), "path.overlap"},
		{"undefined", strings.Replace(s, "[payments]", "[absent]", 1), "reference.module"},
		{"layer forbidden", strings.Replace(s, "allowed_dependencies: [infrastructure]", "allowed_dependencies: []", 1), "dependency.layer"},
		{"cycle", strings.Replace(s, "allowed_dependencies: [storage]", "allowed_dependencies: [orders]", 1), "dependency.cycle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReport("validate", "test", []byte(tc.body))
			Parse([]byte(tc.body), &r)
			if !has(r, tc.rule) {
				t.Fatalf("want %s: %+v", tc.rule, r)
			}
		})
	}
}
func TestGoImplementation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mutate      func(*testing.T, string)
		rule        string
		unavailable bool
	}{
		{name: "valid"},
		{name: "undeclared module", mutate: func(t *testing.T, r string) { write(t, r, "src/application/rogue/x.go", "package rogue") }, rule: "structure.unowned"},
		{name: "loose source", mutate: func(t *testing.T, r string) { write(t, r, "src/loose.go", "package loose") }, rule: "structure.unowned"},
		{name: "missing file", mutate: func(t *testing.T, r string) {
			if e := os.Remove(filepath.Join(r, "src/application/orders/service.go")); e != nil {
				t.Fatal(e)
			}
		}, rule: "structure.missing"},
		{name: "forbidden test import", mutate: func(t *testing.T, r string) {
			write(t, r, "src/application/orders/extra_test.go", "package orders\nimport _ \"example.com/shop/src/infrastructure/storage\"\n")
		}, rule: "dependency.forbidden"},
		{name: "generated tagged import", mutate: func(t *testing.T, r string) {
			write(t, r, "src/application/orders/generated.go", "//go:build custom\n\n// Code generated; DO NOT EDIT.\npackage orders\nimport _ \"example.com/shop/src/infrastructure/storage\"\n")
		}, rule: "dependency.forbidden"},
		{name: "unmanaged local import", mutate: func(t *testing.T, r string) {
			write(t, r, "src/application/orders/extra.go", "package orders\nimport _ \"example.com/shop/other\"\n")
		}, rule: "dependency.unowned"},
		{name: "nested module", mutate: func(t *testing.T, r string) { write(t, r, "src/application/orders/go.mod", "module nested\n") }, unavailable: true},
		{name: "replace", mutate: func(t *testing.T, r string) {
			write(t, r, "go.mod", "module example.com/shop\nreplace example.com/other => ./other\n")
		}, unavailable: true},
		{name: "workspace", mutate: func(t *testing.T, r string) { write(t, r, "go.work", "go 1.24\n") }, unavailable: true},
		{name: "syntax", mutate: func(t *testing.T, r string) { write(t, r, "src/application/orders/broken.go", "not go") }, unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, b := fixture(t)
			if tc.mutate != nil {
				tc.mutate(t, root)
			}
			r := NewReport("check", "test", b)
			d := Parse(b, &r)
			e := d.CheckGo(root, &r)
			if (e != nil) != tc.unavailable {
				t.Fatalf("error %v", e)
			}
			if tc.rule != "" && !has(r, tc.rule) {
				t.Fatalf("missing %s: %+v", tc.rule, r)
			}
			if tc.rule == "" && !tc.unavailable && (len(r.Diagnostics) > 0 || len(r.Edges) != 2) {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestSymlinkRejected(t *testing.T) {
	root, b := fixture(t)
	if e := os.Symlink(t.TempDir(), filepath.Join(root, "src/application/orders/link")); e != nil {
		t.Skip(e)
	}
	r := NewReport("check", "test", b)
	d := Parse(b, &r)
	if e := d.CheckGo(root, &r); e == nil {
		t.Fatal("accepted symlink")
	}
}
func TestRenderEscapesAndLinks(t *testing.T) {
	_, b := fixture(t)
	r := NewReport("render", "test", b)
	d := Parse(b, &r)
	d.Modules[0].Responsibility = "<script>alert(1)</script>"
	var out bytes.Buffer
	if e := Render(&out, d, r); e != nil {
		t.Fatal(e)
	}
	s := out.String()
	for _, want := range []string{"&lt;script&gt;", `data-detail="payments"`, "src/application/orders"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(s, "<script>") {
		t.Fatal("unescaped HTML")
	}
}

func TestPartialDesign(t *testing.T) {
	b := []byte("schema_version: 1\nsource_root: src\nlayers:\n  - id: app\n    directory: app\n    responsibility: application\n    allowed_dependencies: []\nmodules: []\n")
	r := NewReport("validate", "test", b)
	ParseProfile(b, &r, true)
	if len(r.Diagnostics) != 0 {
		t.Fatal(r)
	}
	r = NewReport("validate", "test", b)
	Parse(b, &r)
	if !has(r, "design.incomplete") {
		t.Fatal(r)
	}
}
func TestNestedModuleAboveSource(t *testing.T) {
	root, b := fixture(t)
	r := NewReport("check", "test", b)
	d := Parse(b, &r)
	d.Source = "nested/src"
	write(t, root, "nested/go.mod", "module nested\n")
	write(t, root, "nested/src/application/orders/service.go", "package orders\n")
	write(t, root, "nested/src/application/payments/service.go", "package payments\n")
	write(t, root, "nested/src/infrastructure/storage/repository.go", "package storage\n")
	if e := d.CheckGo(root, &r); e == nil {
		t.Fatal("accepted nested module above source root")
	}
}
