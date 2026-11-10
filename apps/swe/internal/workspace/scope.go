package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const Format = "swe-scope-v1"
const Coverage = "Tracked and non-ignored files, content/type/permissions, submodule index and checked-out revisions and files; explicit writable paths include ignored files. Excludes .git, other ignored files, external files and changes reverted during execution."

type Entry struct {
	Kind    string   `json:"kind"`
	Mode    uint32   `json:"mode"`
	Digest  string   `json:"digest,omitempty"`
	Commits []string `json:"commits,omitempty"`
	Head    string   `json:"head,omitempty"`
}
type Baseline struct {
	Format     string           `json:"format"`
	Root       string           `json:"root"`
	Files      map[string]Entry `json:"files"`
	AllowFiles []string         `json:"allowFiles"`
	AllowDirs  []string         `json:"allowDirs"`
}

func Git(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, e := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Git exceeded 10 seconds: %w", ctx.Err())
	}
	if e != nil {
		return nil, fmt.Errorf("Git %v: %w: %s", args, e, stderr.String())
	}
	return b, nil
}
func Root(value string) (string, error) {
	p, e := filepath.Abs(value)
	if e != nil {
		return "", e
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	b, e := Git(p, "rev-parse", "--show-toplevel")
	if e != nil {
		return "", e
	}
	actual, e := filepath.EvalSymlinks(strings.TrimSpace(string(b)))
	if e != nil {
		return "", e
	}
	if p != actual {
		return "", errors.New("use the Git root, not a subdirectory")
	}
	return p, nil
}
func Inside(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && !filepath.IsAbs(r) && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
func noSymlinkParents(root, path string, include bool) error {
	if !Inside(root, path) {
		return fmt.Errorf("path outside root: %s", path)
	}
	if !include {
		path = filepath.Dir(path)
	}
	for path != root {
		st, e := os.Lstat(path)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in path: %s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return errors.New("invalid root")
		}
		path = parent
	}
	return nil
}
func Normalize(root, value string) (string, error) {
	p := value
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	if !Inside(root, p) || p == root {
		return "", errors.New("writable scope must be inside root and cannot cover the whole repository")
	}
	rel, _ := filepath.Rel(root, p)
	rel = filepath.ToSlash(rel)
	for _, part := range strings.Split(rel, "/") {
		if part == ".git" {
			return "", errors.New("Git internals cannot be writable scope")
		}
	}
	if e := noSymlinkParents(root, p, true); e != nil {
		return "", e
	}
	return rel, nil
}
func walk(root, path string, names map[string]bool) error {
	return filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			rel, e := filepath.Rel(root, p)
			if e != nil {
				return e
			}
			names[filepath.ToSlash(rel)] = true
		}
		return nil
	})
}
func Inventory(root string, extras []string) (map[string]Entry, error) {
	actual, e := Root(root)
	if e != nil || actual != root {
		return nil, fmt.Errorf("invalid Git root: %s: %v", root, e)
	}
	output, e := Git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if e != nil {
		return nil, e
	}
	names := map[string]bool{}
	for _, name := range strings.Split(string(output), "\x00") {
		if name != "" {
			names[name] = true
		}
	}
	index, e := Git(root, "ls-files", "--stage", "-z")
	if e != nil {
		return nil, e
	}
	links := map[string][]string{}
	for _, line := range strings.Split(string(index), "\x00") {
		if line == "" {
			continue
		}
		meta, name, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, errors.New("invalid Git index entry")
		}
		if fields[0] == "160000" {
			links[name] = append(links[name], fields[2]+":"+fields[1])
			names[name] = true
		}
	}
	result := map[string]Entry{}
	for name, commits := range links {
		p := filepath.Join(root, filepath.FromSlash(name))
		if e := noSymlinkParents(root, p, false); e != nil {
			return nil, e
		}
		slices.Sort(commits)
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			result[name] = Entry{Kind: "gitlink", Commits: commits}
			delete(names, name)
			continue
		}
		if e != nil {
			return nil, e
		}
		if !st.IsDir() {
			continue
		}
		b, e := Git(p, "rev-parse", "--show-toplevel")
		if e != nil {
			return nil, e
		}
		child, e := filepath.EvalSymlinks(strings.TrimSpace(string(b)))
		if e != nil {
			return nil, e
		}
		entry := Entry{Kind: "gitlink", Mode: mode(st.Mode()), Commits: commits}
		if child == p {
			b, e = Git(p, "rev-parse", "--verify", "HEAD")
			if e != nil {
				return nil, e
			}
			entry.Head = strings.TrimSpace(string(b))
			children, e := Inventory(p, nil)
			if e != nil {
				return nil, e
			}
			for k, v := range children {
				result[name+"/"+k] = v
			}
		} else {
			if e := walk(root, p, names); e != nil {
				return nil, e
			}
		}
		result[name] = entry
		delete(names, name)
	}
	for _, name := range extras {
		p := filepath.Join(root, filepath.FromSlash(name))
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if st.IsDir() {
			if e := walk(root, p, names); e != nil {
				return nil, e
			}
		} else {
			names[name] = true
		}
	}
	for name := range names {
		p := filepath.Join(root, filepath.FromSlash(name))
		if e := noSymlinkParents(root, p, false); e != nil {
			return nil, e
		}
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		item := Entry{Mode: mode(st.Mode())}
		hash := sha256.New()
		if st.Mode()&os.ModeSymlink != 0 {
			item.Kind = "symlink"
			target, e := os.Readlink(p)
			if e != nil {
				return nil, e
			}
			hash.Write([]byte(target))
		} else if st.Mode().IsRegular() {
			item.Kind = "file"
			f, e := os.Open(p)
			if e != nil {
				return nil, e
			}
			_, e = io.Copy(hash, f)
			ce := f.Close()
			if e != nil {
				return nil, e
			}
			if ce != nil {
				return nil, ce
			}
		} else {
			return nil, fmt.Errorf("unsupported file type: %s", name)
		}
		item.Digest = hex.EncodeToString(hash.Sum(nil))
		result[name] = item
	}
	return result, nil
}
func mode(m os.FileMode) uint32 {
	v := uint32(m.Perm())
	if m&os.ModeSetuid != 0 {
		v |= 04000
	}
	if m&os.ModeSetgid != 0 {
		v |= 02000
	}
	if m&os.ModeSticky != 0 {
		v |= 01000
	}
	return v
}
func guardSubmodules(files map[string]Entry, allowedFiles, allowedDirs []string) error {
	for name, item := range files {
		if item.Kind != "gitlink" {
			continue
		}
		for _, p := range append(slices.Clone(allowedFiles), allowedDirs...) {
			if p == name || strings.HasPrefix(p, name+"/") {
				return fmt.Errorf("scope enters submodule %s; use its own Git root", name)
			}
		}
		for _, p := range allowedDirs {
			if strings.HasPrefix(name, p+"/") {
				return fmt.Errorf("scope includes submodule %s", name)
			}
		}
	}
	return nil
}
func Fingerprint(root string) (string, int, error) {
	files, e := Inventory(root, nil)
	if e != nil {
		return "", 0, e
	}
	b, e := json.Marshal(struct {
		Format, Root string
		Files        map[string]Entry
	}{Format, root, files})
	if e != nil {
		return "", 0, e
	}
	sum := sha256.Sum256(b)
	return Format + ":" + hex.EncodeToString(sum[:]), len(files), nil
}
func ValidateFingerprint(value string) error {
	prefix := Format + ":"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return errors.New("unsupported fingerprint; keep the original tool for an existing review and start a new review explicitly")
	}
	_, e := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return e
}
func baselinePath(root, path string) (string, error) {
	p, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(p))
	if e != nil {
		return "", e
	}
	p = filepath.Join(parent, filepath.Base(p))
	if Inside(root, p) {
		return "", errors.New("baseline must be a new file outside the project")
	}
	if st, e := os.Lstat(p); e == nil && st.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("baseline cannot be a symlink")
	}
	return p, nil
}
func Snapshot(root, path string, files, dirs []string) error {
	if len(files)+len(dirs) == 0 {
		return errors.New("explicit writable scope required")
	}
	var e error
	files = slices.Clone(files)
	dirs = slices.Clone(dirs)
	for i, p := range files {
		files[i], e = Normalize(root, p)
		if e != nil {
			return e
		}
	}
	for i, p := range dirs {
		dirs[i], e = Normalize(root, p)
		if e != nil {
			return e
		}
	}
	items, e := Inventory(root, append(slices.Clone(files), dirs...))
	if e != nil {
		return e
	}
	if e = guardSubmodules(items, files, dirs); e != nil {
		return e
	}
	p, e := baselinePath(root, path)
	if e != nil {
		return e
	}
	b, e := json.Marshal(Baseline{Format, root, items, files, dirs})
	if e != nil {
		return e
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func Check(root, path string) ([]string, []string, error) {
	p, e := baselinePath(root, path)
	if e != nil {
		return nil, nil, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return nil, nil, e
	}
	var before Baseline
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&before); e != nil {
		return nil, nil, e
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, nil, errors.New("trailing baseline data")
	}
	if before.Format != Format || before.Root != root || before.Files == nil || len(before.AllowFiles)+len(before.AllowDirs) == 0 {
		return nil, nil, errors.New("unsupported or invalid baseline; never recreate the baseline during an active task")
	}
	for _, value := range append(slices.Clone(before.AllowFiles), before.AllowDirs...) {
		normalized, e := Normalize(root, value)
		if e != nil {
			return nil, nil, e
		}
		if normalized != value {
			return nil, nil, errors.New("noncanonical baseline scope")
		}
	}
	if e = guardSubmodules(before.Files, before.AllowFiles, before.AllowDirs); e != nil {
		return nil, nil, e
	}
	after, e := Inventory(root, append(slices.Clone(before.AllowFiles), before.AllowDirs...))
	if e != nil {
		return nil, nil, e
	}
	if e = guardSubmodules(after, before.AllowFiles, before.AllowDirs); e != nil {
		return nil, nil, e
	}
	names := map[string]bool{}
	for name := range before.Files {
		names[name] = true
	}
	for name := range after {
		names[name] = true
	}
	changed, unexpected := []string{}, []string{}
	for name := range names {
		a, ao := before.Files[name]
		b, bo := after[name]
		ab, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		if ao == bo && bytes.Equal(ab, bb) {
			continue
		}
		changed = append(changed, name)
		allowed := slices.Contains(before.AllowFiles, name)
		for _, dir := range before.AllowDirs {
			allowed = allowed || name == dir || strings.HasPrefix(name, dir+"/")
		}
		if !allowed {
			unexpected = append(unexpected, name)
		}
	}
	slices.Sort(changed)
	slices.Sort(unexpected)
	return changed, unexpected, nil
}
