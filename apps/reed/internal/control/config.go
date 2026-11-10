package control

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Config binds one service to one explicitly selected workspace and goal.
// AgentCommand must be a host adapter implementing the documented JSON protocol.
type Config struct {
	AgentBackend      string              `json:"agent_backend"`
	CodexModel        string              `json:"codex_model"`
	CodexBaseURL      string              `json:"codex_base_url"`
	Codex             string              `json:"codex"`
	CheckCommands     map[string][]string `json:"check_commands"`
	StatusField       string              `json:"status_field"`
	StatusOptions     map[string]string   `json:"status_options"`
	Root              string              `json:"root"`
	StateDir          string              `json:"state_dir"`
	SkillsDir         string              `json:"skills_dir"`
	SkillVersion      string              `json:"skill_version"`
	SWE               string              `json:"swe"`
	SWEVersion        string              `json:"swe_version"`
	GH                string              `json:"gh"`
	Repository        string              `json:"repository"`
	ProjectOwner      string              `json:"project_owner"`
	ProjectNumber     int                 `json:"project_number"`
	AllowGitHubWrite  bool                `json:"allow_github_write"`
	AgentCommand      []string            `json:"agent_command"`
	AllowedPaths      []string            `json:"allowed_paths"`
	MaxRuns           int                 `json:"max_runs"`
	RunTimeoutSeconds int                 `json:"run_timeout_seconds"`
	IntervalSeconds   int                 `json:"interval_seconds"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return c, errors.New("single JSON configuration required")
	}
	return c, c.Normalize()
}
func (c *Config) Normalize() error {
	var err error
	if c.Root == "" {
		return errors.New("root is required")
	}
	c.Root, err = filepath.EvalSymlinks(c.Root)
	if err != nil {
		return err
	}
	c.Root, err = filepath.Abs(c.Root)
	if err != nil {
		return err
	}
	info, err := os.Stat(c.Root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("root must be a directory")
	}
	if c.StateDir == "" {
		return errors.New("state_dir is required")
	}
	c.StateDir, err = filepath.Abs(c.StateDir)
	if err != nil {
		return err
	}
	if rel, e := filepath.Rel(c.Root, c.StateDir); e == nil && filepath.IsLocal(rel) {
		return errors.New("state_dir must be outside the managed workspace")
	}
	if c.SkillsDir != "" {
		c.SkillsDir, err = filepath.EvalSymlinks(c.SkillsDir)
		if err != nil {
			return err
		}
		c.SkillsDir, err = filepath.Abs(c.SkillsDir)
		if err != nil {
			return err
		}
	}
	if c.AgentBackend != "" && c.AgentBackend != "codex" {
		return errors.New("agent_backend must be codex or empty for a custom host")
	}
	if c.AgentBackend != "" && len(c.AgentCommand) > 0 {
		return errors.New("select one Agent backend")
	}
	if c.Codex == "" {
		c.Codex = "codex"
	}
	if c.StatusField == "" {
		c.StatusField = "Status"
	}
	if c.StatusOptions == nil {
		c.StatusOptions = map[string]string{"ready": "Todo", "active": "In Progress", "done": "Done"}
	}
	if c.SWE == "" {
		c.SWE = "swe"
	}
	if c.GH == "" {
		c.GH = "gh"
	}
	if c.Repository != "" && !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(c.Repository) {
		return errors.New("invalid repository")
	}
	if (c.ProjectOwner == "") != (c.ProjectNumber == 0) || c.ProjectNumber < 0 {
		return errors.New("project_owner and positive project_number must be supplied together")
	}
	if c.MaxRuns < 1 {
		c.MaxRuns = 20
	}
	if c.RunTimeoutSeconds < 1 {
		c.RunTimeoutSeconds = 300
	}
	if c.IntervalSeconds < 0 {
		return errors.New("interval_seconds cannot be negative")
	}
	for _, p := range c.AllowedPaths {
		if !filepath.IsLocal(p) {
			return errors.New("allowed_paths must be relative workspace paths")
		}
	}
	return nil
}
func (c Config) timeout() time.Duration { return time.Duration(c.RunTimeoutSeconds) * time.Second }
