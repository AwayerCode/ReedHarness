package systemdesign

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type goImport struct {
	Target string
	Line   int
}

func readGoImports(file string) ([]goImport, error) {
	set := token.NewFileSet()
	f, err := parser.ParseFile(set, file, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	imports := []goImport{}
	for _, imp := range f.Imports {
		target, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		imports = append(imports, goImport{target, set.Position(imp.Pos()).Line})
	}
	return imports, nil
}

// checkGoImports resolves filesystem ownership before applying dependency rules.
func (d *Design) checkGoImports(imports []goImport, rel string, owner *Module, scope Scope, env *goEnvironment, observed map[string][]string, r *Report) error {
	root, moduleRoot, module := env.Root, env.ModuleRoot, env.Module
	foreignModules, uncertain := env.ForeignModules, env.Uncertain
	for _, imp := range imports {
		target := imp.Target
		foreign := false
		matchLength := -1
		if target == module || strings.HasPrefix(target, module+"/") {
			matchLength = len(module)
		}
		for _, prefix := range foreignModules {
			if (target == prefix || strings.HasPrefix(target, prefix+"/")) && len(prefix) > matchLength {
				foreign = true
				matchLength = len(prefix)
			}
		}
		if foreign {
			r.Add("dependency.cross-scope", rel, "import targets another local system-design scope: "+target)
			continue
		}
		if target != module && !strings.HasPrefix(target, module+"/") {
			if len(uncertain) > 0 && !standardImport(target) {
				return fmt.Errorf("cannot classify import %s while local module metadata is unavailable: %s", target, strings.Join(uncertain, "; "))
			}
			continue
		}
		moduleLocal := strings.TrimPrefix(strings.TrimPrefix(target, module), "/")
		local, err := filepath.Rel(root, filepath.Join(moduleRoot, filepath.FromSlash(moduleLocal)))
		if err != nil {
			return err
		}
		local = filepath.ToSlash(local)
		if local == ".." || strings.HasPrefix(local, "../") || scope.Delegated(local) {
			r.Add("dependency.cross-scope", rel, "import leaves this system-design scope: "+target)
			continue
		}
		to := d.owner(local)
		if to == nil {
			r.Add("dependency.unowned", fmt.Sprintf("%s:%d", rel, imp.Line), "local import outside managed modules: "+target)
			continue
		}
		// A nested module or symlink cannot be accepted as an ordinary local package.
		info, e := regularPath(root, local)
		if e != nil {
			if os.IsNotExist(e) {
				r.Add("dependency.missing", rel, "local import directory missing: "+target)
				continue
			}
			return e
		}
		if !info.IsDir() {
			r.Add("dependency.missing", rel, "local import is not a directory: "+target)
			continue
		}
		if to.ID == owner.ID {
			continue
		}
		recordDependency(owner, to, rel, imp.Line, observed, r)
	}
	return nil
}
