package systemdesign

import (
	"testing"
)

func typeScriptProject(t *testing.T, ordersBody string) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	write(t, root, Filename, languageNeutralDesign)
	write(t, root, "src/application/orders/index.ts", ordersBody)
	write(t, root, "src/infrastructure/storage/index.ts", "export const save = () => undefined;\n")
	return root, []byte(languageNeutralDesign)
}

func TestTypeScriptAdapterParsesStaticReferences(t *testing.T) {
	root, data := typeScriptProject(t, `import type { Storage } from "../../infrastructure/storage";
export { save } from "../../infrastructure/storage/index";
const storage = require("../../infrastructure/storage/index");
void import("../../infrastructure/storage/index");
import React from "react";
// import "../../application/orders/commented-out";
/* export { missing } from "../../application/orders/block-commented-out"; */
export default { from: "../../application/orders/not-an-import" };
const adapter = { require: "../../application/orders/not-a-require" };
adapter.require("../../application/orders/not-a-require");
adapter.import("../../application/orders/not-an-import");
`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 {
		t.Fatalf("%+v", r)
	}
	if len(r.Edges) != 4 {
		t.Fatalf("want four TypeScript edges, got %+v", r.Edges)
	}
	for _, edge := range r.Edges {
		if edge.From != "orders" || edge.To != "storage" {
			t.Fatalf("unexpected edge: %+v", edge)
		}
	}
	if !containsString(r.Coverage, "TypeScript files parsed") {
		t.Fatalf("missing TypeScript coverage: %+v", r.Coverage)
	}
	if !containsString(r.Unverified, "bare TypeScript imports") {
		t.Fatalf("external import classification missing: %+v", r.Unverified)
	}
}

func TestTypeScriptAdapterRejectsForbiddenDependency(t *testing.T) {
	root, data := typeScriptProject(t, `export {};`)
	write(t, root, "src/infrastructure/storage/index.ts", `export { orders } from "../../application/orders/index";`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if !has(r, "dependency.forbidden") || len(r.Edges) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestTypeScriptAdapterResolvesTsconfigPaths(t *testing.T) {
	root, data := typeScriptProject(t, `import { save } from "@domain/infrastructure/storage";`)
	write(t, root, "tsconfig.json", `{
  // JSONC comments and trailing commas are valid TypeScript config.
  "compilerOptions": {
    "baseUrl": "./src",
    "paths": {
      "@domain/*": ["*"],
    },
  },
}
`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || len(r.Edges) != 1 || r.Edges[0].To != "storage" {
		t.Fatalf("%+v", r)
	}
}

func TestTypeScriptAdapterReportsMissingAndAmbiguousReferences(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		extra   string
		rule    string
		wantErr bool
	}{
		{
			name: "missing relative",
			body: `import "./missing";`,
			rule: "dependency.missing",
		},
		{
			name:    "ambiguous extension",
			body:    `import "./dependency";`,
			extra:   "dependency.tsx",
			wantErr: true,
		},
		{
			name:    "unterminated comment",
			body:    "/* no close",
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, data := typeScriptProject(t, tc.body)
			write(t, root, "src/application/orders/dependency.ts", "export {};\n")
			if tc.extra != "" {
				write(t, root, "src/application/orders/"+tc.extra, "export {};\n")
			}
			r := NewReport("check", "test", data)
			d := Parse(data, &r)
			err := d.checkImplementation(root, &r, NewSession())
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v report=%+v", err, r)
			}
			if tc.rule != "" && !has(r, tc.rule) {
				t.Fatalf("missing %s: %+v", tc.rule, r)
			}
		})
	}
}

func TestTypeScriptAdapterMarksNonLiteralDynamicImportUnverified(t *testing.T) {
	root, data := typeScriptProject(t, `const target = "./later"; void import(target);`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if !containsString(r.Unverified, "non-literal dynamic TypeScript imports") {
		t.Fatalf("%+v", r.Unverified)
	}
}

func TestTypeScriptAdapterMarksExtendedConfigIncomplete(t *testing.T) {
	root, data := typeScriptProject(t, `import React from "react";`)
	write(t, root, "tsconfig.json", `{"extends":"./base.json","compilerOptions":{"baseUrl":"./src"}}`)
	r := NewReport("check", "test", data)
	d := Parse(data, &r)
	if err := d.checkImplementation(root, &r, NewSession()); err != nil {
		t.Fatal(err)
	}
	if !containsString(r.Unverified, "inherited path aliases") {
		t.Fatalf("%+v", r.Unverified)
	}
}
