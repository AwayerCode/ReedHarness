package systemdesign

import (
	"os"
	"path/filepath"
)

// Session reuses discovery and Go metadata for one command. It is not shared
// between invocations and does not make checks an atomic filesystem snapshot.
type Session struct {
	discoveries map[string]discoveryResult
	moduleRoots map[string]moduleRootResult
	moduleFiles map[string]moduleFileResult
	inventories map[string]moduleInventory
}
type discoveryResult struct {
	value Discovery
	err   error
}
type moduleRootResult struct {
	root string
	err  error
}
type moduleFileResult struct {
	data []byte
	err  error
}
type moduleInventory struct {
	modules   map[string]string
	uncertain []string
}

func NewSession() *Session {
	return &Session{
		discoveries: map[string]discoveryResult{}, moduleRoots: map[string]moduleRootResult{},
		moduleFiles: map[string]moduleFileResult{}, inventories: map[string]moduleInventory{},
	}
}

func (s *Session) Discover(root string) (Discovery, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Discovery{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Discovery{}, err
	}
	if found, ok := s.discoveries[root]; ok {
		return found.value, found.err
	}
	value, err := Discover(root)
	s.discoveries[root] = discoveryResult{value, err}
	return value, err
}

func (s *Session) findModuleRoot(root string) (string, error) {
	if found, ok := s.moduleRoots[root]; ok {
		return found.root, found.err
	}
	value, err := findModuleRoot(root)
	s.moduleRoots[root] = moduleRootResult{value, err}
	return value, err
}

func (s *Session) readModule(root string) ([]byte, error) {
	if found, ok := s.moduleFiles[root]; ok {
		return found.data, found.err
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	s.moduleFiles[root] = moduleFileResult{data, err}
	return data, err
}
