package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Publication struct {
	Kind      string          `json:"kind"`
	Issue     int             `json:"issue"`
	Task      *TaskContract   `json:"task,omitempty"`
	Body      string          `json:"body"`
	Marker    string          `json:"marker"`
	GoalHash  string          `json:"goal_hash"`
	Attempted map[string]bool `json:"attempted"`
}

func (g GitHub) api(ctx context.Context, method, path string, input any) (json.RawMessage, error) {
	args := []string{"api", path, "--method", method}
	var b []byte
	if input != nil {
		b, _ = json.Marshal(input)
		args = append(args, "--input", "-")
	}
	data, code, err := g.Command(ctx, g.Config.GH, args, g.Config.Root, b)
	if err != nil || code != 0 {
		return nil, fmt.Errorf("GitHub %s result requires verification (%d): %v", method, code, err)
	}
	if !json.Valid(data) {
		return nil, errors.New("invalid GitHub response")
	}
	return data, nil
}
func (g GitHub) pages(ctx context.Context, path string) ([]json.RawMessage, error) {
	raw, err := g.call(ctx, "api", path, "--paginate", "--slurp")
	if err != nil {
		return nil, err
	}
	var pages [][]json.RawMessage
	if err = json.Unmarshal(raw, &pages); err != nil {
		return nil, err
	}
	var all []json.RawMessage
	for _, page := range pages {
		all = append(all, page...)
	}
	return all, nil
}
func findMarked(items []json.RawMessage, marker string) (json.RawMessage, error) {
	var found json.RawMessage
	for _, item := range items {
		var x struct {
			Body    string `json:"body"`
			Content struct {
				Body string `json:"body"`
			} `json:"content"`
		}
		if err := json.Unmarshal(item, &x); err != nil {
			return nil, err
		}
		if strings.Contains(x.Body, marker) || strings.Contains(x.Content.Body, marker) {
			if found != nil {
				return nil, errors.New("duplicate publication markers; reconcile manually")
			}
			found = item
		}
	}
	return found, nil
}
func (g GitHub) write(ctx context.Context, args ...string) error {
	if !g.Config.AllowGitHubWrite {
		return errors.New("GitHub writes disabled")
	}
	_, code, err := g.Command(ctx, g.Config.GH, args, g.Config.Root, nil)
	if err != nil || code != 0 {
		return fmt.Errorf("GitHub write result requires verification (%d): %v", code, err)
	}
	return nil
}
func (g GitHub) projectItems(ctx context.Context) ([]json.RawMessage, error) {
	raw, err := g.call(ctx, "project", "item-list", strconv.Itoa(g.Config.ProjectNumber), "--owner", g.Config.ProjectOwner, "--limit", "1000", "--format", "json")
	if err != nil {
		return nil, err
	}
	var list struct {
		Items      []json.RawMessage `json:"items"`
		TotalCount int               `json:"totalCount"`
	}
	if err = json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	if list.TotalCount > len(list.Items) {
		return nil, errors.New("incomplete Project observation")
	}
	return list.Items, nil
}
func (g GitHub) setStatus(ctx context.Context, issue int, state string) error {
	optionName := g.Config.StatusOptions[state]
	if optionName == "" {
		return fmt.Errorf("missing status mapping for %s", state)
	}
	raw, err := g.call(ctx, "project", "field-list", strconv.Itoa(g.Config.ProjectNumber), "--owner", g.Config.ProjectOwner, "--limit", "1000", "--format", "json")
	if err != nil {
		return err
	}
	var fields struct {
		Fields []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Options []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"options"`
		} `json:"fields"`
	}
	if err = json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	field, option := "", ""
	for _, f := range fields.Fields {
		if f.Name == g.Config.StatusField {
			field = f.ID
			for _, o := range f.Options {
				if o.Name == optionName {
					option = o.ID
				}
			}
		}
	}
	if field == "" || option == "" {
		return errors.New("configured Project status field or option missing")
	}
	items, err := g.projectItems(ctx)
	if err != nil {
		return err
	}
	id := ""
	url := "https://github.com/" + g.Config.Repository + "/issues/" + strconv.Itoa(issue)
	for _, item := range items {
		var i struct {
			ID      string `json:"id"`
			Content struct {
				URL string `json:"url"`
			} `json:"content"`
		}
		if err = json.Unmarshal(item, &i); err != nil {
			return err
		}
		if i.Content.URL == url {
			id = i.ID
		}
	}
	if id == "" {
		return errors.New("task missing from configured Project")
	}
	project, err := g.Project(ctx)
	if err != nil {
		return err
	}
	var p struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(project, &p); err != nil {
		return err
	}
	return g.write(ctx, "project", "item-edit", "--project-id", p.ID, "--id", id, "--field-id", field, "--single-select-option-id", option, "--format", "json")
}
func (e *Engine) stage(ctx context.Context, event Event, approved string, result Decision, p Publication) (Decision, error) {
	p.GoalHash = approved
	p.Marker = "<!-- reed:" + digest([]string{e.config.Repository, approved, event.ID}) + " -->"
	p.Attempted = map[string]bool{}
	if len(p.Body) > 50000 {
		p.Body = p.Body[:50000] + "\n[Full evidence retained in the local run record]"
	}
	p.Body += "\n\n" + p.Marker
	if err := e.updateRun(event.ID, func(r *Run) { r.Result = &result; r.Publication = &p; r.Status = "publishing" }); err != nil {
		return result, err
	}
	return e.publish(ctx, event, approved)
}
func (e *Engine) publish(ctx context.Context, event Event, approved string) (Decision, error) {
	var r Run
	for _, v := range e.Snapshot().Runs {
		if v.Event.ID == event.ID {
			r = v
		}
	}
	if r.Result == nil || r.Publication == nil {
		return Decision{}, errors.New("missing publication checkpoint")
	}
	result := *r.Result
	p := r.Publication
	g := GitHub{e.config, e.command}
	if !e.config.AllowGitHubWrite {
		return result, errors.New("GitHub writes disabled")
	}
	latest, err := g.Project(ctx)
	if err != nil {
		return result, err
	}
	hash, err := goalDigest(latest)
	if err != nil || hash != approved || hash != p.GoalHash {
		return result, errors.New("goal changed; cannot publish stale result")
	}
	if p.Issue > 0 && r.TaskHash != "" {
		raw, err := g.Issue(ctx, p.Issue)
		if err != nil {
			return result, err
		}
		var issue struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err = json.Unmarshal(raw, &issue); err != nil {
			return result, err
		}
		updated := p.Kind == "update" && p.Task != nil && issue.Title == p.Task.Title && issue.Body == taskBody(*p.Task)
		if digest(issue) != r.TaskHash && !updated {
			return result, errors.New("task changed; cannot publish stale acceptance")
		}
	}
	if p.Kind == "complete" {
		tree, err := files(r.Workspace)
		if err != nil {
			return result, err
		}
		if digest(tree) != r.Tree || !checksPassed(r.Checks) || result.Verdict != "pass" {
			return result, errors.New("delivery or acceptance evidence changed before completion")
		}
	}
	attempt := func(key string) error {
		if p.Attempted[key] {
			return errors.New("unconfirmed prior " + key + "; inspect GitHub before explicitly authorizing another attempt")
		}
		p.Attempted[key] = true
		return e.updateRun(event.ID, func(r *Run) { r.Publication = copyPublication(p) })
	}
	endpoint := "repos/" + e.config.Repository + "/issues"
	switch p.Kind {
	case "create":
		items, err := g.pages(ctx, endpoint+"?state=all&per_page=100")
		if err != nil {
			return result, err
		}
		found, err := findMarked(items, p.Marker)
		if err != nil {
			return result, err
		}
		if found == nil {
			if err = attempt("create_issue"); err != nil {
				return result, err
			}
			found, err = g.api(ctx, "POST", endpoint, map[string]string{"title": p.Task.Title, "body": taskBody(*p.Task) + "\n" + p.Marker})
			if err != nil {
				return result, err
			}
		}
		var issue struct {
			Title  string `json:"title"`
			Body   string `json:"body"`
			Number int    `json:"number"`
			URL    string `json:"html_url"`
		}
		if err = json.Unmarshal(found, &issue); err != nil || issue.Number < 1 {
			return result, errors.New("created Issue identity unavailable")
		}
		if issue.Title != p.Task.Title || issue.Body != taskBody(*p.Task)+"\n"+p.Marker {
			return result, errors.New("publication marker conflicts with task contents")
		}
		p.Issue = issue.Number
		url := "https://github.com/" + e.config.Repository + "/issues/" + strconv.Itoa(issue.Number)
		if err = g.write(ctx, "project", "item-add", strconv.Itoa(e.config.ProjectNumber), "--owner", e.config.ProjectOwner, "--url", url, "--format", "json"); err != nil {
			return result, err
		}
		if err = g.setStatus(ctx, issue.Number, "ready"); err != nil {
			return result, err
		}
		result.Next = &Event{Kind: "execute", Issue: issue.Number, Input: "Execute the authorized task and original acceptance criteria"}
		result.Status = "continue"
	case "update":
		if err = g.Edit(ctx, GitHubEdit{Kind: "task", Number: p.Issue, Title: p.Task.Title, Body: taskBody(*p.Task)}); err != nil {
			return result, err
		}
	case "progress", "complete":
		if err = g.requireTask(ctx, p.Issue); err != nil {
			return result, err
		}
		comments, err := g.pages(ctx, endpoint+"/"+strconv.Itoa(p.Issue)+"/comments?per_page=100")
		if err != nil {
			return result, err
		}
		found, err := findMarked(comments, p.Marker)
		if err != nil {
			return result, err
		}
		if found != nil && markedBody(found) != p.Body {
			return result, errors.New("publication marker conflicts with comment evidence")
		}
		if found == nil {
			if err = attempt("comment"); err != nil {
				return result, err
			}
			if _, err = g.api(ctx, "POST", endpoint+"/"+strconv.Itoa(p.Issue)+"/comments", map[string]string{"body": p.Body}); err != nil {
				return result, err
			}
		}
		status := "active"
		if p.Kind == "complete" {
			status = "done"
		}
		if err = g.setStatus(ctx, p.Issue, status); err != nil {
			return result, err
		}
		if p.Kind == "complete" {
			if _, err = g.api(ctx, "PATCH", endpoint+"/"+strconv.Itoa(p.Issue), map[string]string{"state": "closed", "state_reason": "completed"}); err != nil {
				return result, err
			}
		}
	case "evaluation":
		items, err := g.projectItems(ctx)
		if err != nil {
			return result, err
		}
		found, err := findMarked(items, p.Marker)
		if err != nil {
			return result, err
		}
		if found != nil && markedBody(found) != p.Body {
			return result, errors.New("publication marker conflicts with evaluation evidence")
		}
		if found == nil {
			if err = attempt("evaluation"); err != nil {
				return result, err
			}
			if err = g.write(ctx, "project", "item-create", strconv.Itoa(e.config.ProjectNumber), "--owner", e.config.ProjectOwner, "--title", "Reed goal evaluation", "--body", p.Body, "--format", "json"); err != nil {
				return result, err
			}
		}
	default:
		return result, errors.New("unknown publication action")
	}
	// Keep the checkpoint until the final result is durably stored by takeAndRun.
	if err = e.updateRun(event.ID, func(r *Run) { r.Result = &result; r.Publication = copyPublication(p) }); err != nil {
		return result, err
	}
	return result, nil
}
func (e *Engine) RetrySync(id, evidence string, confirmedAbsent bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state.Halted {
		return errors.New("goal stopped")
	}
	for i := range e.state.Runs {
		r := &e.state.Runs[i]
		if r.Event.ID == id && r.Status == "sync_failed" {
			if confirmedAbsent {
				if strings.TrimSpace(evidence) == "" {
					return errors.New("evidence confirming absence is required")
				}
				r.Publication.Attempted = map[string]bool{}
				r.Error = evidence
			}
			for j := range e.state.Runs {
				if e.state.Runs[j].Status == "interrupted" {
					return errors.New("reconcile interrupted work first")
				}
			}
			r.Status = "publishing"
			e.state.Paused = false
			if err := e.saveLocked(); err != nil {
				return err
			}
			e.signal()
			return nil
		}
	}
	return errors.New("failed publication not found")
}

func copyPublication(p *Publication) *Publication {
	b, _ := json.Marshal(p)
	var copy Publication
	json.Unmarshal(b, &copy)
	return &copy
}

func markedBody(raw json.RawMessage) string {
	var item struct {
		Body    string `json:"body"`
		Content struct {
			Body string `json:"body"`
		} `json:"content"`
	}
	json.Unmarshal(raw, &item)
	if item.Body != "" {
		return item.Body
	}
	return item.Content.Body
}
