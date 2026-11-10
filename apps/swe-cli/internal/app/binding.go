package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// RunInstalled verifies the CLI runtime binding beside its bin directory.
// Developer binaries without a binding remain usable.
func RunInstalled(args []string, in io.Reader, out io.Writer) int {
	executable, e := os.Executable()
	if e != nil {
		return unavailable(out, e)
	}
	if e = verifyBinding(executable); e != nil {
		return unavailable(out, e)
	}
	return Run(args, in, out)
}
func verifyBinding(executable string) error {
	if filepath.Base(filepath.Dir(executable)) != "bin" {
		return nil
	}
	data, e := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(executable)), "runtime.json"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var binding struct {
		SchemaVersion int    `json:"schema_version"`
		Name          string `json:"name"`
		Version       string `json:"version"`
		Assets        map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"assets"`
	}
	if e = json.Unmarshal(data, &binding); e != nil {
		return e
	}
	if binding.SchemaVersion != 1 || binding.Name != "swe-cli" || binding.Version != Version {
		return fmt.Errorf("companion version mismatch: tool %s, binding %s; reinstall the bound runtime", Version, binding.Version)
	}
	expected := binding.Assets[runtime.GOOS+"/"+runtime.GOARCH].SHA256
	if len(expected) != 64 {
		return fmt.Errorf("missing companion digest for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	f, e := os.Open(executable)
	if e != nil {
		return e
	}
	defer f.Close()
	hash := sha256.New()
	if _, e = io.Copy(hash, f); e != nil {
		return e
	}
	if !strings.EqualFold(expected, hex.EncodeToString(hash.Sum(nil))) {
		return fmt.Errorf("companion executable differs from runtime binding; reinstall")
	}
	return nil
}
