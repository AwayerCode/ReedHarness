package systemdesign

import (
	"testing"
)

func TestGoAdapterRunsThroughBuiltinPipeline(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root, data := fixture(t)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || len(r.Edges) != 2 {
		t.Fatalf("%+v", r)
	}
	if !containsString(r.Coverage, "3 Go files parsed") {
		t.Fatalf("Go adapter coverage missing: %+v", r.Coverage)
	}
}

func TestGoAdapterParticipatesInMixedLanguageCycle(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	design := `schema_version: 1
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
    required_files: [service.go, index.ts]
    allowed_dependencies: [storage]
  - id: storage
    layer: infrastructure
    directory: storage
    responsibility: 存储访问
    required_files: [repository.go, index.ts]
    allowed_dependencies: []
`
	write(t, root, Filename, design)
	write(t, root, "go.mod", "module example.com/mixed\n\ngo 1.24.0\n")
	write(t, root, "src/application/orders/service.go", `package orders

import _ "example.com/mixed/src/infrastructure/storage"
`)
	write(t, root, "src/application/orders/index.ts", "export {};\n")
	write(t, root, "src/infrastructure/storage/repository.go", "package storage\n")
	write(t, root, "src/infrastructure/storage/index.ts", `export * from "../../application/orders/index";`)
	r := NewReport("check", "test", []byte(design))
	d := Parse([]byte(design), &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Edges) != 2 || !has(r, "dependency.forbidden") || !has(r, "dependency.cycle") {
		t.Fatalf("Go dependency did not join mixed-language state: %+v", r)
	}
}
