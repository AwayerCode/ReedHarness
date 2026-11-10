package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Adoption uses the installed package's commands on the real scope; discovery
// must remain distinct from validation, and checks must not rewrite the design.
func TestAdoptionCommandContracts(t *testing.T) {
	doc, err := os.ReadFile("../../../../skills/swe-adoption/references/existing-project.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []bool{false, true} {
		name := "existing-design"
		if invalid {
			name = "invalid-design"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../systemdesign/testdata/project")); err != nil {
				t.Fatal(err)
			}
			design := filepath.Join(root, "system-design.yaml")
			if invalid {
				if err := os.WriteFile(design, []byte("layers: ["), 0644); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(design)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, line := range strings.Split(string(doc), "\n") {
				if !strings.HasPrefix(line, "reed system-design ") {
					continue
				}
				count++
				command := strings.TrimPrefix(line, "reed ")
				command = strings.NewReplacer("<项目根目录>", root, "<YAML路径>", "system-design.yaml").Replace(command)
				args := strings.Fields(command)
				var out bytes.Buffer
				code := Run(args, nil, &out)
				if invalid && args[1] != "list" {
					if code == 0 {
						t.Fatalf("invalid design passed: %s", command)
					}
				} else if code != 0 {
					t.Fatalf("%s: %d %s", command, code, &out)
				}
				if args[1] == "list" || args[len(args)-1] == "--all" {
					assertBatchResultContract(t, out.Bytes(), args[1])
				}
			}
			if count != 5 {
				t.Fatalf("expected discovery, single and batch validate/check commands, got %d", count)
			}
			after, err := os.ReadFile(design)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("adoption checks changed the authoritative YAML", err)
			}
		})
	}
}
