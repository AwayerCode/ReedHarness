package systemdesign

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGoModSyntaxCannotHideDependencies(t *testing.T) {
	for _, declaration := range []string{
		"module example.com/shop//comment\n",
		"module \"example.com/shop\" // comment\n",
		"module(\nexample.com/shop\n)\n",
	} {
		t.Run(declaration, func(t *testing.T) {
			t.Setenv("GOWORK", "off")
			root, b := fixture(t)
			write(t, root, "go.mod", declaration+"go 1.24.0\n")
			write(t, root, "src/application/orders/forbidden.go", "package orders\nimport _ \"example.com/shop/src/infrastructure/storage\"\n")
			r := NewReport("check", "test", b)
			d := Parse(b, &r)
			if e := d.CheckGo(root, &r); e != nil {
				t.Fatal(e)
			}
			if !has(r, "dependency.forbidden") || len(r.Edges) != 3 {
				t.Fatalf("module syntax hid imports: %+v", r)
			}
		})
	}
}

func TestGoModUnsupportedOrInvalidCannotPass(t *testing.T) {
	for _, body := range []string{
		"module example.com/shop\nreplace(\nexample.com/alias => .\n)\n",
		"module example.com/shop\nreplace example.com/alias => .\n",
		"module example.com/shop\nmodule example.com/other\n",
		"module example.com/shop\nrequire(\ninvalid\n)\n",
		"go 1.24.0\n",
	} {
		t.Run(body, func(t *testing.T) {
			t.Setenv("GOWORK", "off")
			root, b := fixture(t)
			write(t, root, "go.mod", body)
			r := NewReport("check", "test", b)
			d := Parse(b, &r)
			if e := d.CheckGo(root, &r); e == nil {
				t.Fatalf("accepted unsupported/invalid go.mod: %s", body)
			}
		})
	}
}

func TestWorkspaceSelection(t *testing.T) {
	for _, tc := range []struct {
		name, setting              string
		local, external, wantError bool
	}{
		{name: "unset without workspace"},
		{name: "auto without workspace", setting: "auto"},
		{name: "unset local workspace", local: true, wantError: true},
		{name: "auto local workspace", setting: "auto", local: true, wantError: true},
		{name: "off ignores local workspace", setting: "off", local: true},
		{name: "explicit external workspace", external: true, wantError: true},
		{name: "explicit missing workspace", setting: "missing", wantError: true},
		{name: "invalid relative workspace", setting: "relative/go.work", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, b := fixture(t)
			if tc.local {
				write(t, root, "go.work", "go 1.24.0\n")
			}
			setting := tc.setting
			if tc.external {
				external := t.TempDir()
				write(t, external, "go.work", "go 1.24.0\n")
				setting = filepath.Join(external, "go.work")
			}
			if setting == "missing" {
				setting = filepath.Join(t.TempDir(), "missing.work")
			}
			t.Setenv("GOWORK", setting)
			r := NewReport("check", "test", b)
			d := Parse(b, &r)
			e := d.CheckGo(root, &r)
			if (e != nil) != tc.wantError {
				t.Fatalf("GOWORK=%q: error=%v, report=%+v", setting, e, r)
			}
			if e == nil && (len(r.Diagnostics) != 0 || len(r.Edges) != 2) {
				t.Fatal(r)
			}
		})
	}
}

func TestRequiredFilesAreRealizable(t *testing.T) {
	_, b := fixture(t)
	for _, tc := range []struct {
		name, files string
		conflict    bool
	}{
		{"file then child", "[service.go, service.go/child.go]", true},
		{"child then file", "[service.go/child.go, service.go]", true},
		{"case folded parent", "[SERVICE.GO/child.go, service.go]", true},
		{"case folded duplicate", "[service.go, Service.go]", true},
		{"nested ancestor", "[a/b/c.go, a/b]", true},
		{"shared parent", "[internal/a.go, internal/b.go]", false},
		{"similar prefix", "[service.go, service.go-backup/child.go]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(strings.Replace(string(b), "[service.go]", tc.files, 1))
			r := NewReport("validate", "test", data)
			Parse(data, &r)
			if has(r, "path.file-conflict") != tc.conflict {
				t.Fatalf("files %s: %+v", tc.files, r)
			}
			if !tc.conflict && len(r.Diagnostics) > 0 {
				t.Fatal(r)
			}
		})
	}
}
