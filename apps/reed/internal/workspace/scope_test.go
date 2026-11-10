package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	run(t, root, "init", "-q")
	run(t, root, "config", "user.name", "Test")
	run(t, root, "config", "user.email", "test@example.invalid")
	put(t, root, "tracked", "original")
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "initial")
	return root
}
func run(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
}
func put(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, name)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestScopeGuards(t *testing.T) {
	root := repo(t)
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	put(t, root, ".gitignore", "ignored/\n")
	put(t, root, "ignored/a", "one")
	if e := Snapshot(root, baseline, []string{"tracked"}, []string{"ignored"}); e != nil {
		t.Fatal(e)
	}
	if e := Snapshot(root, baseline, []string{"tracked"}, nil); e == nil {
		t.Fatal("overwrote baseline")
	}
	put(t, root, "tracked", "changed")
	put(t, root, "ignored/a", "changed")
	changed, unexpected, e := Check(root, baseline)
	if e != nil || len(changed) != 2 || len(unexpected) != 0 {
		t.Fatal(changed, unexpected, e)
	}
	put(t, root, "outside", "new")
	if e = os.Remove(filepath.Join(root, ".gitignore")); e != nil {
		t.Fatal(e)
	}
	_, unexpected, e = Check(root, baseline)
	if e != nil || len(unexpected) != 2 {
		t.Fatal(unexpected, e)
	}
	for _, p := range []string{".", "../outside", ".git/config"} {
		if _, e = Normalize(root, p); e == nil {
			t.Fatal("allowed", p)
		}
	}
	if e = Snapshot(root, filepath.Join(root, "baseline.json"), []string{"tracked"}, nil); e == nil {
		t.Fatal("in-project baseline")
	}
	put(t, filepath.Dir(baseline), "old.json", `{"root":"old","files":{}}`)
	if _, _, e = Check(root, filepath.Join(filepath.Dir(baseline), "old.json")); e == nil {
		t.Fatal("old baseline accepted")
	}
}
func TestReadonlyAndSymlinks(t *testing.T) {
	root := repo(t)
	a, _, e := Fingerprint(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateFingerprint(a); e != nil {
		t.Fatal(e)
	}
	if e = ValidateFingerprint(strings.Repeat("a", 64)); e == nil {
		t.Fatal("unversioned fingerprint accepted")
	}
	put(t, root, "tracked", "change")
	b, _, e := Fingerprint(root)
	if e != nil || a == b {
		t.Fatal("content change missed", e)
	}
	if runtime.GOOS != "windows" {
		if e = os.Chmod(filepath.Join(root, "tracked"), 0700); e != nil {
			t.Fatal(e)
		}
		c, _, e := Fingerprint(root)
		if e != nil || b == c {
			t.Fatal("mode change missed", e)
		}
	}
	outside := t.TempDir()
	if e = os.Symlink(outside, filepath.Join(root, "link")); e != nil {
		t.Log("symlink unavailable:", e)
		return
	}
	if _, e = Normalize(root, "link/file"); e == nil {
		t.Fatal("symlink writable path accepted")
	}
	if _, e = Inventory(root, nil); e != nil {
		t.Fatal(e)
	}
}
func TestSubmodules(t *testing.T) {
	root, child := repo(t), repo(t)
	run(t, root, "-c", "protocol.file.allow=always", "submodule", "add", "-q", child, "module")
	run(t, root, "commit", "-qam", "module")
	before, e := Inventory(root, nil)
	if e != nil {
		t.Fatal(e)
	}
	if before["module"].Kind != "gitlink" || before["module/tracked"].Kind != "file" {
		t.Fatal(before)
	}
	if e = Snapshot(root, filepath.Join(t.TempDir(), "baseline"), nil, []string{"module"}); e == nil {
		t.Fatal("submodule scope accepted")
	}
	put(t, filepath.Join(root, "module"), "untracked", "new")
	after, e := Inventory(root, nil)
	if e != nil || after["module/untracked"].Kind != "file" {
		t.Fatal(after, e)
	}
	run(t, root, "submodule", "deinit", "-f", "--", "module")
	put(t, filepath.Join(root, "module"), "ordinary", "kept")
	after, e = Inventory(root, nil)
	if e != nil || after["module"].Head != "" || after["module/ordinary"].Kind != "file" {
		t.Fatal(after, e)
	}
}
func TestGitFailure(t *testing.T) {
	if _, e := Inventory(t.TempDir(), nil); e == nil {
		t.Fatal("nonrepository accepted")
	}
}
