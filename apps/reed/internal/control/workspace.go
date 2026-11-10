package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func files(root string) (map[string]string, error) {
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		var data []byte
		if info.Mode()&os.ModeSymlink != 0 {
			link, e := os.Readlink(path)
			data = []byte(link)
			err = e
		} else if info.Mode().IsRegular() {
			data, err = os.ReadFile(path)
		} else {
			return fmt.Errorf("unsupported workspace entry: %s", rel)
		}
		if err != nil {
			return err
		}
		h := sha256.Sum256(data)
		result[rel] = fmt.Sprintf("%o:%x", info.Mode(), h)
		return nil
	})
	return result, err
}
func scopeChanged(before, after map[string]string, paths []string) error {
	for p, v := range before {
		if after[p] != v && !within(p, paths) {
			return fmt.Errorf("unauthorized modification: %s", p)
		}
	}
	for p, v := range after {
		if before[p] != v && !within(p, paths) {
			return fmt.Errorf("unauthorized modification: %s", p)
		}
	}
	return nil
}
func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	b, c, e := Execute(ctx, "git", args, dir, nil)
	if e != nil || c != 0 {
		return nil, fmt.Errorf("git %v: %s %v", args, b, e)
	}
	return b, nil
}
func (e *Engine) taskWorkspace(ctx context.Context, issue int, taskHash string) (string, error) {
	snapshot := e.Snapshot()
	for i := len(snapshot.Runs) - 1; i >= 0; i-- {
		r := snapshot.Runs[i]
		if r.Event.Issue == issue && r.Workspace != "" {
			if r.TaskHash != taskHash {
				return "", fmt.Errorf("task definition changed; create a new task to preserve the original acceptance and delivery")
			}
			return r.Workspace, nil
		}
	}
	status, err := git(ctx, e.config.Root, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if len(status) != 0 {
		return "", fmt.Errorf("source workspace must be clean before preparing a task checkout")
	}
	head, err := git(ctx, e.config.Root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	base := filepath.Join(e.config.StateDir, "workspaces")
	if err = os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	workspace := filepath.Join(base, "issue-"+strconv.Itoa(issue))
	if _, err = os.Stat(workspace); !os.IsNotExist(err) {
		return "", fmt.Errorf("unrecorded workspace exists: %s; reconcile before retry", workspace)
	}
	if _, err = git(ctx, e.config.Root, "clone", "--no-hardlinks", "--no-checkout", "--", e.config.Root, workspace); err != nil {
		return "", err
	}
	if _, err = git(ctx, workspace, "checkout", "--detach", string(bytesTrim(head))); err != nil {
		return "", err
	}
	return workspace, nil
}
func bytesTrim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
func (e *Engine) updateRun(id string, update func(*Run)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.state.Runs {
		if e.state.Runs[i].Event.ID == id {
			update(&e.state.Runs[i])
			return e.saveLocked()
		}
	}
	return fmt.Errorf("run not found")
}
func (e *Engine) runChecks(ctx context.Context, names []string, root string) []CheckResult {
	results := []CheckResult{}
	for _, name := range names {
		command := e.config.CheckCommands[name]
		r := CheckResult{Name: name, ExitCode: 2, Command: append([]string{}, command...)}
		if len(command) > 0 && command[0] == e.config.SWE {
			b, code, err := e.command(ctx, e.config.SWE, []string{"version"}, root, nil)
			var tool struct {
				Version string `json:"version"`
			}
			if err != nil || code != 0 || json.Unmarshal(b, &tool) != nil || e.config.SWEVersion == "" || tool.Version != e.config.SWEVersion {
				r.Error = "swe version unavailable or mismatched"
				results = append(results, r)
				continue
			}
			r.Version = tool.Version
		}
		if len(command) == 0 {
			r.Error = "unconfigured check"
		} else {
			args := append([]string{}, command[1:]...)
			for i, a := range args {
				if a == "{workspace}" {
					args[i] = root
				}
			}
			b, c, err := e.command(ctx, command[0], args, root, nil)
			r.Output = string(b)
			r.ExitCode = c
			if err != nil {
				r.Error = err.Error()
			}
		}
		results = append(results, r)
		if ctx.Err() != nil {
			break
		}
	}
	return results
}
