package systemdesign

import (
	"encoding/json"
	"strings"
	"testing"
)

const cppDesign = `schema_version: 1
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
    required_files: [main.cpp]
    allowed_dependencies: [storage]
  - id: storage
    layer: infrastructure
    directory: storage
    responsibility: 存储访问
    required_files: [include/domain/storage.hpp]
    allowed_dependencies: []
`

func cppProject(t *testing.T, source string) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	write(t, root, Filename, cppDesign)
	write(t, root, "src/application/orders/main.cpp", source)
	write(t, root, "src/infrastructure/storage/include/domain/storage.hpp", "#pragma once\n")
	return root, []byte(cppDesign)
}

func TestCppAdapterResolvesQuotedAndSourceRootIncludes(t *testing.T) {
	root, data := cppProject(t, `// #include "../../missing.hpp"
/* #include "../../also-missing.hpp" */
#include "../../infrastructure/storage/include/domain/storage.hpp"
#include <domain/storage.hpp>
#include <vector>
`)
	database := []map[string]any{{
		"directory": root,
		"arguments": []string{"clang++", "-isystem", "src/infrastructure/storage/include", "-c", "src/application/orders/main.cpp"},
	}}
	encoded, err := json.Marshal(database)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "compile_commands.json", string(encoded))
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || len(r.Edges) != 2 {
		t.Fatalf("%+v", r)
	}
	if !containsString(r.Coverage, "C++ source/header files parsed") {
		t.Fatalf("%+v", r.Coverage)
	}
	if !containsString(r.Unverified, "treated as external") {
		t.Fatalf("%+v", r.Unverified)
	}
}

func TestCppAdapterUsesCompileCommandsIncludeDirectories(t *testing.T) {
	root, data := cppProject(t, `#include <domain/storage.hpp>`)
	database := []map[string]any{{
		"directory": root,
		"arguments": []string{"clang++", "-I", "src/infrastructure/storage/include", "-c", "src/application/orders/main.cpp"},
	}}
	encoded, err := json.Marshal(database)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "compile_commands.json", string(encoded))
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || len(r.Edges) != 1 || r.Edges[0].To != "storage" {
		t.Fatalf("%+v", r)
	}
}

func TestCppAdapterResolvesSourceRootIncludes(t *testing.T) {
	root, data := cppProject(t, `#include <infrastructure/storage/include/domain/storage.hpp>`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || len(r.Edges) != 1 || r.Edges[0].To != "storage" {
		t.Fatalf("%+v", r)
	}
}

func TestCppAdapterDoesNotGuessUniquePathSuffixes(t *testing.T) {
	root, data := cppProject(t, `#include <domain/storage.hpp>`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || len(r.Edges) != 0 {
		t.Fatalf("unconfigured include must remain external: %+v", r)
	}
	if !containsString(r.Unverified, "treated as external") {
		t.Fatalf("missing unresolved include boundary: %+v", r.Unverified)
	}
}

func TestCppAdapterReportsForbiddenAndMissingReferences(t *testing.T) {
	root, data := cppProject(t, `#include "../../infrastructure/storage/include/domain/storage.hpp"
#include "../../missing.hpp"
`)
	write(t, root, "src/infrastructure/storage/include/domain/storage.hpp", `#include "../../../../application/orders/main.cpp"`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if !has(r, "dependency.forbidden") || !has(r, "dependency.missing") {
		t.Fatalf("%+v", r)
	}
}

func TestCppAdapterRejectsMacroInclude(t *testing.T) {
	root, data := cppProject(t, `#include HEADER`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	err := d.checkImplementation(root, &r, NewSession())
	if err == nil || !strings.Contains(err.Error(), "macro-expanded") {
		t.Fatalf("want macro include failure: %v", err)
	}
}

func TestMixedAdaptersRecordCrossLanguageEdges(t *testing.T) {
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
    required_files: [index.ts, orders.hpp]
    allowed_dependencies: [storage]
  - id: storage
    layer: infrastructure
    directory: storage
    responsibility: 存储访问
    required_files: [index.ts, storage.hpp]
    allowed_dependencies: []
`
	write(t, root, Filename, design)
	write(t, root, "src/application/orders/index.ts", `export * from "../../infrastructure/storage/index";`)
	write(t, root, "src/application/orders/orders.hpp", `#include "../../infrastructure/storage/storage.hpp"`)
	write(t, root, "src/infrastructure/storage/index.ts", "export {};\n")
	write(t, root, "src/infrastructure/storage/storage.hpp", "#pragma once\n")
	r := NewReport("check", "test", []byte(design))
	d := Parse([]byte(design), &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || len(r.Edges) != 2 {
		t.Fatalf("want independent TypeScript and C++ edges: %+v", r)
	}
	for _, edge := range r.Edges {
		if edge.From != "orders" || edge.To != "storage" {
			t.Fatalf("unexpected edge: %+v", edge)
		}
	}
}

func TestMixedAdaptersDetectCrossLanguageCycle(t *testing.T) {
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
    required_files: [index.ts, main.cpp]
    allowed_dependencies: [storage]
  - id: storage
    layer: infrastructure
    directory: storage
    responsibility: 存储访问
    required_files: [storage.hpp]
    allowed_dependencies: []
`
	write(t, root, Filename, design)
	write(t, root, "src/application/orders/index.ts", `export * from "../../infrastructure/storage/index";`)
	write(t, root, "src/application/orders/main.cpp", "#include <vector>\n")
	write(t, root, "src/infrastructure/storage/index.ts", "export {};\n")
	write(t, root, "src/infrastructure/storage/storage.hpp", `#include "../../application/orders/main.cpp"`)
	r := NewReport("check", "test", []byte(design))
	d := Parse([]byte(design), &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if !has(r, "dependency.forbidden") || !has(r, "dependency.cycle") {
		t.Fatalf("cross-language cycle not reported: %+v", r)
	}
}
