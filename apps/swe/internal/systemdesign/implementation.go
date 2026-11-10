package systemdesign

import (
	"errors"
	"fmt"
	"path/filepath"
)

// sourceAdapter is the optional language-specific layer. Core structure and
// dependency policy remain owned by this package so adapters cannot redefine
// YAML semantics.
type sourceAdapter interface {
	Name() string
	Matches(sourceInventory) bool
	Check(*Design, sourceInventory, *Report, *Session, *sourceAdapterState) error
}

type sourceAdapterState struct {
	observed map[string][]string
}

func builtinSourceAdapters() []sourceAdapter {
	return []sourceAdapter{goSourceAdapter{}, typeScriptSourceAdapter{}, cppSourceAdapter{}}
}

// CheckGo keeps the direct Go adapter entry point for callers that explicitly
// request Go analysis. Normal system-design checks use checkImplementation.
func (d *Design) CheckGo(root string, r *Report) error {
	return d.checkImplementationWithAdapters(root, r, NewSession(), []sourceAdapter{goSourceAdapter{}})
}

// checkImplementation first validates language-neutral structure, then runs
// every adapter that recognizes the owned files.
func (d *Design) checkImplementation(root string, r *Report, session *Session) error {
	return d.checkImplementationWithAdapters(root, r, session, builtinSourceAdapters())
}

func (d *Design) checkImplementationWithAdapters(root string, r *Report, session *Session, adapters []sourceAdapter) error {
	root, e := filepath.Abs(root)
	if e != nil {
		return e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return e
	}
	scope := Scope{Root: root, Children: []string{}}
	if r.Scope != nil {
		scope = *r.Scope
	} else {
		scopes, err := session.Discover(root)
		if err != nil {
			return err
		}
		for _, s := range scopes.Scopes {
			if s.Root == root {
				scope = s
			}
		}
	}
	if scope.Unavailable != "" {
		return errors.New(scope.Unavailable)
	}
	d.ValidateScope(scope, r)
	if len(r.Diagnostics) > 0 {
		return nil
	}
	inventory, e := d.inspectSource(root, scope, r)
	if e != nil {
		return e
	}
	if len(r.Diagnostics) > 0 {
		return nil
	}
	matched := false
	state := &sourceAdapterState{observed: map[string][]string{}}
	for _, adapter := range adapters {
		if !adapter.Matches(inventory) {
			continue
		}
		matched = true
		if e := adapter.Check(d, inventory, r, session, state); e != nil {
			return fmt.Errorf("%s source adapter: %w", adapter.Name(), e)
		}
	}
	cycles(state.observed, "observed module", r)
	if matched {
		r.Unverified = append(r.Unverified, "Source languages without a matching adapter, if present, are not dependency-checked.")
	} else {
		r.Unverified = append(r.Unverified, "No source-language adapter matched; imports and source dependencies are not checked.")
	}
	return nil
}
