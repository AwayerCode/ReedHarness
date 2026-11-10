package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type GitHub struct {
	Config  Config
	Command Command
}

func (g GitHub) call(ctx context.Context, args ...string) (json.RawMessage, error) {
	data, code, err := g.Command(ctx, g.Config.GH, args, g.Config.Root, nil)
	if err != nil || code != 0 {
		return nil, fmt.Errorf("GitHub command failed (%d): %v", code, err)
	}
	if !json.Valid(data) {
		return nil, errors.New("GitHub returned invalid JSON")
	}
	return data, nil
}
func (g GitHub) Project(ctx context.Context) (json.RawMessage, error) {
	if g.Config.ProjectNumber == 0 {
		return nil, errors.New("no GitHub Project configured")
	}
	return g.call(ctx, "project", "view", strconv.Itoa(g.Config.ProjectNumber), "--owner", g.Config.ProjectOwner, "--format", "json")
}
func (g GitHub) Tasks(ctx context.Context) (json.RawMessage, error) {
	if g.Config.ProjectNumber == 0 {
		return nil, errors.New("no GitHub Project configured")
	}
	// Project membership is the task boundary, rather than every Issue in a repository.
	raw, err := g.call(ctx, "project", "item-list", strconv.Itoa(g.Config.ProjectNumber), "--owner", g.Config.ProjectOwner, "--limit", "1000", "--format", "json")
	if err != nil {
		return nil, err
	}
	var items struct {
		Items      []json.RawMessage `json:"items"`
		TotalCount int               `json:"totalCount"`
	}
	if err = json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	if items.TotalCount > len(items.Items) {
		return nil, errors.New("Project exceeds the 1000-item observation limit; refusing an incomplete task snapshot")
	}
	filtered := []json.RawMessage{}
	for _, item := range items.Items {
		var v struct {
			Content struct {
				URL string `json:"url"`
			} `json:"content"`
		}
		if err = json.Unmarshal(item, &v); err != nil {
			return nil, err
		}
		if g.Config.Repository != "" && strings.HasPrefix(v.Content.URL, "https://github.com/"+g.Config.Repository+"/issues/") {
			filtered = append(filtered, item)
		}
	}
	return json.Marshal(map[string]any{"items": filtered, "totalCount": len(filtered)})
}
func (g GitHub) Issue(ctx context.Context, number int) (json.RawMessage, error) {
	if g.Config.Repository == "" || number < 1 {
		return nil, errors.New("repository and positive issue number required")
	}
	if err := g.requireTask(ctx, number); err != nil {
		return nil, err
	}
	return g.call(ctx, "issue", "view", strconv.Itoa(number), "--repo", g.Config.Repository, "--json", "number,title,body,state,url,comments,labels")
}

type GitHubEdit struct {
	Kind   string `json:"kind"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func (g GitHub) Edit(ctx context.Context, e GitHubEdit) error {
	if !g.Config.AllowGitHubWrite {
		return errors.New("GitHub writes disabled in service configuration")
	}
	var args []string
	switch e.Kind {
	case "goal":
		if g.Config.ProjectNumber == 0 {
			return errors.New("no Project configured")
		}
		args = []string{"project", "edit", strconv.Itoa(g.Config.ProjectNumber), "--owner", g.Config.ProjectOwner, "--title", e.Title, "--readme", e.Body}
	case "task":
		if g.Config.Repository == "" || e.Number < 1 {
			return errors.New("repository and positive issue number required")
		}
		if err := g.requireTask(ctx, e.Number); err != nil {
			return err
		}
		args = []string{"issue", "edit", strconv.Itoa(e.Number), "--repo", g.Config.Repository, "--title", e.Title, "--body", e.Body}
	default:
		return errors.New("unknown GitHub edit kind")
	}
	_, code, err := g.Command(ctx, g.Config.GH, args, g.Config.Root, nil)
	if err != nil || code != 0 {
		return fmt.Errorf("GitHub write result requires verification (%d): %v", code, err)
	}
	return nil
}

func (g GitHub) requireTask(ctx context.Context, number int) error {
	raw, err := g.Tasks(ctx)
	if err != nil {
		return err
	}
	var items struct {
		Items []struct {
			Content struct {
				URL string `json:"url"`
			} `json:"content"`
		} `json:"items"`
	}
	if err = json.Unmarshal(raw, &items); err != nil {
		return err
	}
	want := "https://github.com/" + g.Config.Repository + "/issues/" + strconv.Itoa(number)
	for _, item := range items.Items {
		if item.Content.URL == want {
			return nil
		}
	}
	return errors.New("Issue is not in the configured Project and repository")
}

// Management edits share the scheduler's gate; an edit cannot race a new dispatch.
func (e *Engine) Edit(ctx context.Context, change GitHubEdit) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.config.AllowGitHubWrite {
		return errors.New("GitHub writes disabled in service configuration")
	}
	if e.active != nil {
		return errors.New("pause and wait for active work before editing business definitions")
	}
	if change.Kind == "task" {
		for _, r := range e.state.Runs {
			if r.Event.Issue == change.Number && r.Workspace != "" {
				return errors.New("task execution has started; create a new task instead of changing its original acceptance")
			}
		}
	}
	if change.Kind == "goal" {
		e.state.Paused = true
		if err := e.saveLocked(); err != nil {
			return err
		}
	}
	return (GitHub{e.config, e.command}).Edit(ctx, change)
}
