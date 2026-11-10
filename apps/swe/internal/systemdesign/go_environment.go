package systemdesign

import (
	"errors"
	"fmt"
	"go/build"
	"golang.org/x/mod/modfile"
	"os"
	"path"
	"path/filepath"
	"slices"
)

type goEnvironment struct {
	Root, ModuleRoot, Module  string
	ForeignModules, Uncertain []string
}

func (session *Session) loadGoEnvironment(root, source string, r *Report) (*goEnvironment, error) {
	moduleRoot, e := session.findModuleRoot(root)
	if e != nil {
		return nil, e
	}
	info, e := regularPath(moduleRoot, "go.mod")
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("go.mod must be a regular file")
	}
	b, e := session.readModule(moduleRoot)
	if e != nil {
		return nil, e
	}
	parsed, e := modfile.Parse("go.mod", b, nil)
	if e != nil {
		return nil, fmt.Errorf("parse go.mod: %w", e)
	}
	if parsed.Module == nil || parsed.Module.Mod.Path == "" {
		return nil, fmt.Errorf("go.mod requires a module directive")
	}
	module := parsed.Module.Mod.Path
	foreignModules, uncertain, e := session.localScopeModules(root, moduleRoot)
	if e != nil {
		return nil, e
	}
	for _, issue := range uncertain {
		r.Unverified = append(r.Unverified, "Local module metadata unavailable: "+issue)
	}
	// Replacements change package ownership and require a resolver beyond v1.
	if len(parsed.Replace) > 0 {
		return nil, fmt.Errorf("go.mod replace directives are not supported by system-design v1")
	}
	if e := checkWorkspace(root); e != nil {
		return nil, e
	}
	for p := path.Dir(source); p != "."; p = path.Dir(p) {
		if _, e := os.Lstat(filepath.Join(root, filepath.FromSlash(p), "go.mod")); e == nil {
			return nil, fmt.Errorf("nested Go module above source_root: %s", p)
		} else if !os.IsNotExist(e) {
			return nil, e
		}
	}
	return &goEnvironment{root, moduleRoot, module, foreignModules, uncertain}, nil
}

// Respect Go's workspace selection, including an explicit path outside root's ancestors.
func checkWorkspace(root string) error {
	switch setting := os.Getenv("GOWORK"); setting {
	case "off":
		return nil
	case "", "auto":
		for p := root; ; p = filepath.Dir(p) {
			if _, e := os.Lstat(filepath.Join(p, "go.work")); e == nil {
				return fmt.Errorf("Go workspaces are not supported by system-design v1: %s", p)
			} else if !os.IsNotExist(e) {
				return e
			}
			if filepath.Dir(p) == p {
				return nil
			}
		}
	default:
		if !filepath.IsAbs(setting) {
			return fmt.Errorf("GOWORK must be off, auto or an absolute path: %s", setting)
		}
		return fmt.Errorf("explicit GOWORK is not supported by system-design v1: %s", setting)
	}
}

// A design may manage a subdirectory of a larger Go module.
var errNoModule = errors.New("no Go module")

func findModuleRoot(root string) (string, error) {
	for p := root; ; p = filepath.Dir(p) {
		info, err := os.Lstat(filepath.Join(p, "go.mod"))
		if err == nil {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("go.mod must be regular: %s", p)
			}
			return p, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			return "", fmt.Errorf("%w within repository for %s", errNoModule, root)
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if filepath.Dir(p) == p {
			return "", fmt.Errorf("%w in ancestors for %s", errNoModule, root)
		}
	}
}

// Other independently managed Go modules in the same repository are not
// silently treated as third-party libraries. No dependency code is executed.
func (session *Session) localScopeModules(root, moduleRoot string) ([]string, []string, error) {
	search := moduleRoot
	for p := root; ; p = filepath.Dir(p) {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			search = p
			break
		} else if !os.IsNotExist(err) {
			return nil, nil, err
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	inventory, cached := session.inventories[search]
	if !cached {
		discovery, err := session.Discover(search)
		if err != nil {
			return nil, nil, err
		}
		inventory = moduleInventory{modules: map[string]string{}, uncertain: []string{}}
		for _, issue := range discovery.Issues {
			inventory.uncertain = append(inventory.uncertain, issue.Path+": "+issue.Message)
		}
		for _, scope := range discovery.Scopes {
			other, err := session.findModuleRoot(scope.Root)
			if err != nil {
				if !errors.Is(err, errNoModule) {
					inventory.uncertain = append(inventory.uncertain, scope.Root+": "+err.Error())
				}
				continue
			}
			if _, ok := inventory.modules[other]; ok {
				continue
			}
			b, err := session.readModule(other)
			if err != nil {
				inventory.uncertain = append(inventory.uncertain, other+": "+err.Error())
				continue
			}
			modulePath := modfile.ModulePath(b)
			if modulePath != "" {
				inventory.modules[other] = modulePath
			} else {
				inventory.uncertain = append(inventory.uncertain, "missing module path: "+other)
			}
		}
		session.inventories[search] = inventory
	}
	result := []string{}
	uncertain := inventory.uncertain
	for other, modulePath := range inventory.modules {
		if other != moduleRoot {
			result = append(result, modulePath)
		}
	}
	slices.Sort(result)
	return result, uncertain, nil
}

// Classify standard-library imports without running go list or project code.
func standardImport(target string) bool {
	if target == "C" {
		return true
	} // cgo pseudo-package, not another Go module
	pkg, err := build.Default.Import(target, "", build.FindOnly)
	return err == nil && pkg.Goroot
}
