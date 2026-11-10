package checks

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGeneratedSchemasMatchSource(t *testing.T) {
	var generated map[string]any
	if e := json.Unmarshal([]byte(SchemaJSON), &generated); e != nil {
		t.Fatal(e)
	}
	files, e := filepath.Glob("../../resources/artifact-schemas/*.json")
	if e != nil || len(files) == 0 {
		t.Fatal("missing canonical schemas", e)
	}
	canonical := map[string]any{}
	for _, p := range files {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		var s any
		if e = json.Unmarshal(b, &s); e != nil {
			t.Fatal(e)
		}
		name := filepath.Base(p)
		canonical[name[:len(name)-5]] = s
	}
	if !reflect.DeepEqual(generated, canonical) {
		t.Fatal("run go generate ./... to refresh compiled schemas")
	}
}

func TestArchitectureTemplate(t *testing.T) {
	root := "../../../../skills/swe-system-design"
	var schemas map[string]Schema
	if e := json.Unmarshal([]byte(SchemaJSON), &schemas); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(root, "assets/architecture-design.md"))
	if e != nil {
		t.Fatal(e)
	}
	headings := []string{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			headings = append(headings, line[3:])
		}
	}
	if !reflect.DeepEqual(headings, schemas["system-design"].Profiles["architecture"]) {
		t.Fatal("architecture template differs from owned sections")
	}
}

func TestSkillLinksResolve(t *testing.T) {
	root := "../../../../skills"
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		ignored := []string{}
		for _, m := range linkRE.FindAllStringSubmatch(strings.Join(Visible(string(b), &ignored), "\n"), -1) {
			u, e := url.Parse(strings.Trim(m[1], "<>"))
			if e != nil {
				return e
			}
			if u.Scheme != "" || u.Host != "" || u.Path == "" {
				continue
			}
			target := filepath.Join(filepath.Dir(path), filepath.FromSlash(u.Path))
			if _, e = os.Stat(target); e != nil {
				t.Errorf("%s: %s: %v", path, m[1], e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Each Skill must remain usable when no sibling Skill is installed.
func TestIndependentSkillClosure(t *testing.T) {
	entries, err := os.ReadDir("../../../../skills")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join("../../../../skills", entry.Name(), "SKILL.md")); err != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		t.Fatal("no Skill entry found")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			if err := os.CopyFS(root, os.DirFS(filepath.Join("../../../../skills", name))); err != nil {
				t.Fatal(err)
			}
			err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				if entry.Name() == "runtime.json" || entry.Name() == "swe-cli" || entry.Name() == "swe-cli.exe" {
					t.Errorf("CLI must be independently distributed: %s", path)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if filepath.Ext(path) == ".json" && !json.Valid(data) {
					t.Errorf("invalid JSON: %s", path)
				}
				if entry.Name() == "contract.json" {
					if err := checkContract(path, root); err != nil {
						t.Error(err)
					}
				}
				if filepath.Ext(path) != ".md" {
					return nil
				}
				for _, sibling := range names {
					text := strings.NewReplacer("swe-system-design-v1", "CLI_PROTOCOL", "swe-system-design-batch-v1", "CLI_BATCH_PROTOCOL").Replace(string(data))
					if sibling != name && strings.Contains(text, sibling) {
						t.Errorf("%s knows sibling %s", path, sibling)
					}
				}
				ignored := []string{}
				for _, match := range linkRE.FindAllStringSubmatch(strings.Join(Visible(string(data), &ignored), "\n"), -1) {
					u, err := url.Parse(strings.Trim(match[1], "<>"))
					if err != nil {
						return err
					}
					if u.Scheme != "" || u.Host != "" || u.Path == "" {
						continue
					}
					target := filepath.Join(filepath.Dir(path), filepath.FromSlash(u.Path))
					relative, err := filepath.Rel(root, target)
					if err != nil {
						return err
					}
					if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
						t.Errorf("%s references outside own Skill: %s", path, match[1])
						continue
					}
					if _, err := os.Stat(target); err != nil {
						t.Errorf("%s: missing isolated resource %s", path, match[1])
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A Skill contract may only point at resources inside its own directory.
func checkContract(name, root string) error {
	data, e := os.ReadFile(name)
	if e != nil {
		return e
	}
	var contract struct {
		Version int               `json:"schema_version"`
		Inputs  []string          `json:"inputs"`
		Outputs []string          `json:"outputs"`
		Schemas map[string]string `json:"schemas"`
	}
	if e = json.Unmarshal(data, &contract); e != nil {
		return e
	}
	if contract.Version != 1 {
		return fmt.Errorf("%s: unsupported schema_version %d", name, contract.Version)
	}
	for _, kind := range append(append([]string{}, contract.Inputs...), contract.Outputs...) {
		if _, ok := contract.Schemas[kind]; !ok {
			return fmt.Errorf("%s: %s declares no schema", name, kind)
		}
	}
	for kind, ref := range contract.Schemas {
		target := filepath.Join(filepath.Dir(name), filepath.FromSlash(ref))
		relative, e := filepath.Rel(root, target)
		if e != nil {
			return e
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s: %s references outside own Skill: %s", name, kind, ref)
		}
		schema, e := os.ReadFile(target)
		if e != nil {
			return fmt.Errorf("%s: %s schema unavailable: %w", name, kind, e)
		}
		if !json.Valid(schema) {
			return fmt.Errorf("%s: %s schema is not valid JSON", name, kind)
		}
	}
	return nil
}

// Reference copies exist so a Skill stays readable on its own. They must not
// drift from the CLI-owned machine schemas that actually enforce the format.
func TestSkillSchemaCopiesMatchCLIResources(t *testing.T) {
	copies, err := filepath.Glob("../../../../skills/*/references/artifact-schemas/*.json")
	if err != nil || len(copies) == 0 {
		t.Fatal("missing Skill schema copies", err)
	}
	yamlCopies, err := filepath.Glob("../../../../skills/*/references/system-design/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, copyPath := range append(copies, yamlCopies...) {
		relative, err := filepath.Rel(filepath.Join("..", "..", "..", "..", "skills"), copyPath)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		canonical := filepath.Join("..", "..", "resources", filepath.Join(parts[2:]...))
		copyData, err := os.ReadFile(copyPath)
		if err != nil {
			t.Fatal(err)
		}
		canonicalData, err := os.ReadFile(canonical)
		if err != nil {
			t.Fatalf("%s: CLI resource unavailable: %v", copyPath, err)
		}
		if string(copyData) != string(canonicalData) {
			t.Errorf("%s differs from %s; copy the CLI resource instead of editing the Skill copy", copyPath, canonical)
		}
	}
}

// docs/cli.md is the single CLI reference; Skill copies differ only in two links.
func TestSkillCLIReferencesMatchDocs(t *testing.T) {
	document, err := os.ReadFile("../../../../docs/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		"(../skills/swe-system-design/references/system-design/guide.md", "(system-design/guide.md",
	).Replace(string(document))
	copies, err := filepath.Glob("../../../../skills/*/references/cli.md")
	if err != nil || len(copies) == 0 {
		t.Fatal("missing Skill CLI references", err)
	}
	for _, copyPath := range copies {
		data, err := os.ReadFile(copyPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("%s differs from docs/cli.md; update the reference rather than the copy", copyPath)
		}
	}
}
