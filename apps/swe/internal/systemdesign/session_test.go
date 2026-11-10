package systemdesign

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionDiscoveryAndMetadataLifetime(t *testing.T) {
	root, _ := fixture(t)
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession()
	first, err := session.Discover(root)
	if err != nil || len(first.Scopes) != 1 {
		t.Fatal(first, err)
	}
	metadata, err := session.readModule(root)
	if err != nil {
		t.Fatal(err)
	}
	// A command uses one discovery/metadata view. A later command must not reuse it.
	write(t, root, "nested/system-design.yaml", "schema_version: 1\n")
	write(t, root, "go.mod", "module example.com/changed\n")
	again, err := session.Discover(root)
	if err != nil || len(again.Scopes) != 1 {
		t.Fatal("discovery was repeated", again, err)
	}
	cached, err := session.readModule(root)
	if err != nil || string(cached) != string(metadata) {
		t.Fatal("metadata was reread", err)
	}
	fresh := NewSession()
	next, err := fresh.Discover(root)
	if err != nil || len(next.Scopes) != 2 {
		t.Fatal("new invocation retained stale scopes", next, err)
	}
	updated, err := fresh.readModule(root)
	if err != nil || string(updated) == string(metadata) {
		t.Fatal("new invocation retained stale metadata", err)
	}
}

func BenchmarkBatchSession(b *testing.B) {
	b.Setenv("GOWORK", "off")
	root := b.TempDir()
	writeFile := func(name, body string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			b.Fatal(err)
		}
	}
	writeFile(".git", "gitdir: unused\n")
	design := `schema_version: 1
source_root: src
layers:
  - id: domain
    directory: domain
    responsibility: Domain
    allowed_dependencies: []
modules:
  - id: core
    layer: domain
    directory: core
    responsibility: Core
    required_files: [core.go]
    allowed_dependencies: []
`
	for i := 0; i < 24; i++ {
		prefix := fmt.Sprintf("service-%02d/", i)
		writeFile(prefix+Filename, design)
		writeFile(prefix+"go.mod", fmt.Sprintf("module example.com/service%d\n", i))
		writeFile(prefix+"src/domain/core/core.go", "package core\n")
		for j := 0; j < 30; j++ {
			writeFile(fmt.Sprintf("%sdocs/note-%d.txt", prefix, j), "note")
		}
	}
	for _, shared := range []bool{false, true} {
		b.Run(fmt.Sprintf("shared=%t", shared), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				session := NewSession()
				discovery, err := session.Discover(root)
				if err != nil {
					b.Fatal(err)
				}
				for _, scope := range discovery.Scopes {
					current := session
					if !shared {
						current = NewSession()
					}
					r, _, code := current.Execute(scope, "check", "", "test")
					if code != 0 {
						b.Fatal(r)
					}
				}
			}
		})
	}
}
