package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type GoalContract struct {
	AllowedPaths   []string `json:"allowed_paths"`
	Checks         []string `json:"checks"`
	EffectChecks   []string `json:"effect_checks"`
	MaxRuns        int      `json:"max_runs"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}
type TaskContract struct {
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Acceptance   []string `json:"acceptance"`
	AllowedPaths []string `json:"allowed_paths"`
	Checks       []string `json:"checks"`
	Skills       []string `json:"skills"`
}
type CheckResult struct {
	Command  []string `json:"command"`
	Version  string   `json:"version,omitempty"`
	Name     string   `json:"name"`
	ExitCode int      `json:"exit_code"`
	Output   string   `json:"output"`
	Error    string   `json:"error,omitempty"`
}

func contract(body, kind string, out any) error {
	marker := "```reed-" + kind + "\n"
	if strings.Count(body, marker) != 1 {
		return fmt.Errorf("exactly one reed-%s JSON block required", kind)
	}
	block := strings.SplitN(body, marker, 2)[1]
	end := strings.Index(block, "\n```")
	if end < 0 {
		return errors.New("unclosed contract block")
	}
	return strictJSON([]byte(block[:end]), out)
}
func strictJSON(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("single JSON value required")
	}
	return nil
}
func within(path string, roots []string) bool {
	if !filepath.IsLocal(path) {
		return false
	}
	path = filepath.Clean(path)
	for _, root := range roots {
		r, err := filepath.Rel(filepath.Clean(root), path)
		if err == nil && filepath.IsLocal(r) {
			return true
		}
	}
	return false
}
func validatePaths(root string, paths, ceiling []string) error {
	for _, p := range paths {
		if !within(p, ceiling) {
			return fmt.Errorf("path %q exceeds authorized scope", p)
		}
		// Reject symlink components even when their target happens to remain inside root.
		at := root
		for _, part := range strings.Split(filepath.Clean(p), string(filepath.Separator)) {
			if part == ".git" || part == ".codex" || part == ".agents" {
				return fmt.Errorf("host metadata is not writable: %s", p)
			}
			at = filepath.Join(at, part)
			info, err := os.Lstat(at)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("authorized path traverses a symlink: %s", p)
			}
		}
	}
	return nil
}
func (c Config) validateGoal(g GoalContract) error {
	if g.MaxRuns < 1 || g.MaxRuns > c.MaxRuns || g.TimeoutSeconds < 1 || g.TimeoutSeconds > c.RunTimeoutSeconds {
		return errors.New("goal budget exceeds service authorization")
	}
	if len(g.EffectChecks) == 0 {
		return errors.New("goal requires fresh effect checks")
	}
	if err := validatePaths(c.Root, g.AllowedPaths, c.AllowedPaths); err != nil {
		return err
	}
	for _, n := range append(append([]string{}, g.Checks...), g.EffectChecks...) {
		if len(c.CheckCommands[n]) == 0 {
			return fmt.Errorf("unknown authorized check %q", n)
		}
	}
	return nil
}
func (c Config) validateTask(t TaskContract, g GoalContract) error {
	if strings.Contains(t.Body, "```reed-task") || strings.Contains(t.Body, "<!-- reed:") {
		return errors.New("task body contains reserved protocol markers")
	}
	if strings.TrimSpace(t.Title) == "" || len(t.Acceptance) == 0 {
		return errors.New("task title and acceptance criteria required")
	}
	for _, a := range t.Acceptance {
		if strings.TrimSpace(a) == "" {
			return errors.New("empty acceptance criterion")
		}
	}
	if err := validatePaths(c.Root, t.AllowedPaths, g.AllowedPaths); err != nil {
		return err
	}
	for _, n := range t.Checks {
		allowed := false
		for _, gname := range g.Checks {
			if n == gname {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("check %q exceeds goal authorization", n)
		}
		if len(c.CheckCommands[n]) == 0 {
			return fmt.Errorf("unknown authorized check %q", n)
		}
	}
	for _, n := range g.Checks {
		found := false
		for _, v := range t.Checks {
			found = found || v == n
		}
		if !found {
			return fmt.Errorf("task omits required check %q", n)
		}
	}
	if len(t.Checks) == 0 {
		return errors.New("task requires at least one acceptance check")
	}
	for _, n := range t.Skills {
		if _, err := ReadSkill(c.SkillsDir, n, "SKILL.md"); err != nil {
			return err
		}
	}
	return nil
}
func taskBody(t TaskContract) string {
	b, _ := json.MarshalIndent(t, "", "  ")
	return t.Body + "\n\n```reed-task\n" + string(b) + "\n```\n"
}
func checksPassed(results []CheckResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r.ExitCode != 0 || r.Error != "" {
			return false
		}
	}
	return true
}
