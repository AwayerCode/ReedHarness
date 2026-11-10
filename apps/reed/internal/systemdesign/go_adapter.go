package systemdesign

import (
	"fmt"
	"path/filepath"
	"strings"
)

type goSourceAdapter struct{}

func (goSourceAdapter) Name() string { return "Go" }

func (goSourceAdapter) Matches(inventory sourceInventory) bool {
	for _, file := range inventory.Files {
		if strings.HasSuffix(file.Relative, ".go") {
			return true
		}
	}
	return false
}

// Check parses all build-tag branches and never executes project code.
func (goSourceAdapter) Check(d *Design, inventory sourceInventory, r *Report, session *Session, state *sourceAdapterState) error {
	for _, dir := range inventory.Directories {
		if filepath.Base(filepath.FromSlash(dir)) == "vendor" {
			return fmt.Errorf("vendor directories inside source_root are unsupported: %s", dir)
		}
	}
	for _, file := range inventory.Files {
		switch filepath.Base(filepath.FromSlash(file.Relative)) {
		case "go.mod", "go.work":
			return fmt.Errorf("nested Go modules/workspaces are unsupported: %s", file.Relative)
		}
	}
	env, err := session.loadGoEnvironment(inventory.Root, d.Source, r)
	if err != nil {
		return err
	}
	goFiles := 0
	for _, file := range inventory.Files {
		if !strings.HasSuffix(file.Relative, ".go") {
			continue
		}
		goFiles++
		imports, err := readGoImports(file.Path)
		if err != nil {
			return err
		}
		if err := d.checkGoImports(imports, file.Relative, file.Owner, inventory.Scope, env, state.observed, r); err != nil {
			return err
		}
	}
	r.Coverage = append(r.Coverage, fmt.Sprintf("%d Go files parsed including tests, generated files and all build tags", goFiles), "Nearest ancestor Go module; child scopes excluded; import declarations only; external imports excluded")
	r.Unverified = append(r.Unverified, "No compiler/type resolution or build execution; build-tag alternatives are conservatively combined; external dependency internals are not inspected.")
	return nil
}
