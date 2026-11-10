package systemdesign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const languageNeutralDesign = `schema_version: 1
source_root: src
layers:
  - id: application
    directory: application
    responsibility: 应用流程
    allowed_dependencies: [infrastructure]
  - id: infrastructure
    directory: infrastructure
    responsibility: 技术资源
    allowed_dependencies: []
modules:
  - id: orders
    layer: application
    directory: orders
    responsibility: 订单处理
    required_files: [index.ts]
    allowed_dependencies: [storage]
  - id: storage
    layer: infrastructure
    directory: storage
    responsibility: 存储访问
    required_files: [index.ts]
    allowed_dependencies: []
`

func writeUnsupportedLanguageProject(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	data := []byte(strings.ReplaceAll(languageNeutralDesign, "index.ts", "index.rs"))
	write(t, root, Filename, string(data))
	write(t, root, "src/application/orders/index.rs", "pub fn orders() {}\n")
	write(t, root, "src/infrastructure/storage/index.rs", "pub fn storage() {}\n")
	return root, data
}

func TestCheckWithoutLanguageAdapterStillValidatesStructure(t *testing.T) {
	root, data := writeUnsupportedLanguageProject(t)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || r.Status != "passed" {
		t.Fatalf("%+v", r)
	}
	if !containsString(r.Unverified, "No source-language adapter matched") {
		t.Fatalf("missing language coverage boundary: %+v", r.Unverified)
	}
	if !containsString(r.Coverage, "regular files remained inside owned modules") {
		t.Fatalf("missing structure coverage: %+v", r.Coverage)
	}
}

func TestBuiltinSourceAdaptersIncludeAllSupportedLanguages(t *testing.T) {
	names := []string{}
	for _, adapter := range builtinSourceAdapters() {
		names = append(names, adapter.Name())
	}
	for _, want := range []string{"Go", "TypeScript", "C++"} {
		if !containsString(names, want) {
			t.Fatalf("missing %s adapter: %+v", want, names)
		}
	}
}

func TestLanguageNeutralStructureViolationsWithoutLanguageAdapter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string)
		rule   string
	}{
		{
			name: "required file",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "src/application/orders/index.rs")); err != nil {
					t.Fatal(err)
				}
			},
			rule: "structure.missing",
		},
		{
			name: "unowned file",
			mutate: func(t *testing.T, root string) {
				write(t, root, "src/application/rogue.ts", "export {};\n")
			},
			rule: "structure.unowned",
		},
		{
			name: "unowned directory",
			mutate: func(t *testing.T, root string) {
				write(t, root, "src/rogue/index.ts", "export {};\n")
			},
			rule: "structure.unowned",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, data := writeUnsupportedLanguageProject(t)
			tc.mutate(t, root)
			r := NewReport("check", "test", data)
			d := Parse(data, &r)
			if err := d.checkImplementation(root, &r, NewSession()); err != nil {
				t.Fatal(err)
			}
			if !has(r, tc.rule) {
				t.Fatalf("missing %s: %+v", tc.rule, r)
			}
		})
	}
}

func TestGoFilesWithoutModuleRemainUnavailable(t *testing.T) {
	root, data := fixture(t)
	if err := os.Remove(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	err := d.checkImplementation(root, &r, NewSession())
	if err == nil || !strings.Contains(err.Error(), "no Go module") {
		t.Fatalf("want unavailable Go import analysis: %v", err)
	}
	if len(r.Diagnostics) != 0 {
		t.Fatalf("structural check should have passed: %+v", r)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
