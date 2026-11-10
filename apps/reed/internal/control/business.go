package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (e *Engine) businessGoal(ctx context.Context, approved string) (json.RawMessage, GoalContract, error) {
	raw, err := (GitHub{e.config, e.command}).Project(ctx)
	var goal GoalContract
	if err != nil {
		return nil, goal, err
	}
	hash, err := goalDigest(raw)
	if err != nil || approved == "" || hash != approved {
		return nil, goal, errors.New("goal definition needs approval")
	}
	var project struct {
		Readme string `json:"readme"`
	}
	if err = json.Unmarshal(raw, &project); err != nil {
		return nil, goal, err
	}
	if err = contract(project.Readme, "goal", &goal); err != nil {
		return nil, goal, err
	}
	return raw, goal, e.config.validateGoal(goal)
}
func (e *Engine) runBusiness(ctx context.Context, event Event, approved string) (result Decision, runErr error) {
	invoked := false
	defer func() {
		if invoked && runErr != nil {
			runErr = fmt.Errorf("%w: %v", errUnknownEffects, runErr)
		}
	}()
	goalRaw, goal, err := e.businessGoal(ctx, approved)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(goal.TimeoutSeconds)*time.Second)
	defer cancel()
	if len(e.Snapshot().Runs) > goal.MaxRuns {
		return result, errors.New("goal run budget exhausted")
	}
	g := GitHub{e.config, e.command}
	if !e.config.AllowGitHubWrite {
		return result, errors.New("business execution requires authorized GitHub writeback")
	}
	var taskRaw json.RawMessage
	var task TaskContract
	workspace := e.config.Root
	taskHash := ""
	tree := ""
	checks := []CheckResult{}
	if event.Issue > 0 {
		taskRaw, err = g.Issue(ctx, event.Issue)
		if err != nil {
			return result, err
		}
		var issue struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			State string `json:"state"`
		}
		if err = json.Unmarshal(taskRaw, &issue); err != nil {
			return result, err
		}
		if strings.EqualFold(issue.State, "closed") && event.Kind != "evaluate" {
			return result, errors.New("task is already closed")
		}
		if err = contract(issue.Body, "task", &task); err != nil {
			return result, err
		}
		if err = e.config.validateTask(task, goal); err != nil {
			return result, err
		}
		taskHash = digest(struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}{issue.Title, issue.Body})
		if event.Kind == "execute" || event.Kind == "check" || event.Kind == "review" {
			workspace, err = e.taskWorkspace(ctx, event.Issue, taskHash)
			if err != nil {
				return result, err
			}
			if err = validatePaths(workspace, task.AllowedPaths, goal.AllowedPaths); err != nil {
				return result, err
			}
			if err = e.updateRun(event.ID, func(r *Run) { r.Workspace = workspace; r.TaskHash = taskHash }); err != nil {
				return result, err
			}
		}
	} else {
		if event.Kind == "execute" || event.Kind == "check" || event.Kind == "review" {
			return result, errors.New("task work requires an Issue")
		}
		taskRaw, err = g.Tasks(ctx)
		if err != nil {
			return result, err
		}
	}
	before, err := files(workspace)
	if err != nil {
		return result, err
	}
	tree = digest(before)
	if event.Kind == "check" || event.Kind == "review" {
		priorKind := "execute"
		if event.Kind == "review" {
			priorKind = "check"
		}
		var prior *Run
		s := e.Snapshot()
		// The most recent work for this task must be the accepted prerequisite; older green runs cannot be reused.
		for i := len(s.Runs) - 1; i >= 0; i-- {
			r := s.Runs[i]
			if r.Event.ID == event.ID || r.Event.Issue != event.Issue || r.Workspace == "" {
				continue
			}
			prior = &r
			break
		}
		if prior == nil || prior.Event.Kind != priorKind || prior.TaskHash != taskHash || prior.Tree != tree || prior.Status != "continue" {
			return result, errors.New("missing current delivery/check evidence for this task revision")
		}
		if event.Kind == "review" {
			if !checksPassed(prior.Checks) {
				return result, errors.New("cannot review failed checks")
			}
			checks = prior.Checks
		}
	}
	if event.Kind == "check" {
		invoked = true
		checks = e.runChecks(ctx, task.Checks, workspace)
		after, err := files(workspace)
		if err != nil {
			return result, err
		}
		// Check commands may generate build output, but evidence is bound to the exact resulting checkout.
		tree = digest(after)
		next := "review"
		summary := "Required checks passed"
		if !checksPassed(checks) {
			next = "execute"
			summary = "Required checks failed; repair the current task"
		}
		result = Decision{Protocol: "reed-agent-v1", Status: "continue", Summary: summary, Next: &Event{Kind: next, Issue: event.Issue, Input: summary}, Evidence: []string{digest(checks)}}
	} else {
		skillList, err := ListSkills(e.config.SkillsDir, e.config.SkillVersion)
		if err != nil {
			return result, err
		}
		if event.Issue > 0 {
			selected := []Skill{}
			for _, skill := range skillList {
				for _, name := range task.Skills {
					if name == skill.Name {
						selected = append(selected, skill)
					}
				}
			}
			skillList = selected
		}
		paths := []string{}
		if event.Kind == "execute" {
			paths = task.AllowedPaths
		}
		effects := []CheckResult{}
		if event.Kind == "evaluate" {
			effects = e.runChecks(ctx, goal.EffectChecks, e.config.Root)
			checks = effects
		}
		request := map[string]any{"protocol": "reed-agent-v1", "event": event, "goal": goalRaw, "task": taskRaw, "goal_contract": goal, "task_contract": task, "history": e.Snapshot().Runs, "root": workspace, "allowed_paths": paths, "skills": skillList, "swe": e.config.SWE, "timeout_seconds": goal.TimeoutSeconds, "fresh_session": true, "read_only": event.Kind != "execute", "github_write": false, "checks": checks, "effect_results": effects}
		invoked = true
		result, err = e.invokeAgent(ctx, request)
		if err != nil {
			return result, err
		}
		after, err := files(workspace)
		if err != nil {
			return result, err
		}
		if err = scopeChanged(before, after, paths); err != nil {
			return result, fmt.Errorf("%w: %v", errUnknownEffects, err)
		}
		tree = digest(after)
		if result.Protocol != "reed-agent-v1" || strings.TrimSpace(result.Summary) == "" || result.Session == "" {
			return result, fmt.Errorf("%w: invalid Agent decision/session", errUnknownEffects)
		}
		if result.Status != "continue" && result.Status != "wait" && result.Status != "needs_input" && result.Status != "done" {
			return result, errors.New("unknown decision status")
		}
		if result.Status == "continue" && (result.Next == nil || !validKind(result.Next.Kind)) {
			return result, errors.New("continue requires next event")
		}
		if result.Status != "continue" && result.Next != nil {
			return result, errors.New("only continue may schedule work")
		}
		if result.Status == "done" && (event.Kind != "evaluate" || len(result.Evidence) == 0 || !checksPassed(effects)) {
			return result, errors.New("goal completion requires newly collected passing effect evidence")
		}
		if result.Task != nil {
			if event.Kind != "plan" && event.Kind != "evaluate" {
				return result, errors.New("only planning or evaluation may propose a task")
			}
			if result.Status != "continue" {
				return result, errors.New("task proposal requires continue")
			}
			if err = e.config.validateTask(*result.Task, goal); err != nil {
				return result, err
			}
		}
		if event.Kind == "execute" && result.Status == "continue" && (result.Next.Kind != "check" || result.Next.Issue != event.Issue) {
			return result, errors.New("delivery must produce a check event for this task")
		}
		if event.Kind == "review" {
			for _, r := range e.Snapshot().Runs {
				if r.Result != nil && r.Result.Session == result.Session {
					return result, errors.New("review reused an earlier Agent session")
				}
			}
			if len(result.Evidence) == 0 {
				return result, errors.New("review requires acceptance evidence")
			}
			switch result.Verdict {
			case "pass":
				result.Status = "continue"
				result.Next = &Event{Kind: "evaluate", Issue: 0, Input: "Task accepted; collect fresh goal effects"}
			case "fail":
				result.Status = "continue"
				result.Next = &Event{Kind: "execute", Issue: event.Issue, Input: result.Summary}
			case "blocked":
				result.Status = "needs_input"
				result.Next = nil
			default:
				return result, errors.New("review requires pass/fail/blocked verdict")
			}
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if _, _, err = e.businessGoal(ctx, approved); err != nil {
		return result, fmt.Errorf("%w: %v", errUnknownEffects, err)
	}
	if err = e.updateRun(event.ID, func(r *Run) { r.TaskHash = taskHash; r.Tree = tree; r.Checks = checks }); err != nil {
		return result, err
	}
	if result.Next != nil && result.Next.Issue != event.Issue && result.Next.Issue > 0 {
		raw, err := g.Issue(ctx, result.Next.Issue)
		if err != nil {
			return result, err
		}
		var item struct {
			Body string `json:"body"`
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return result, err
		}
		var proposed TaskContract
		if err = contract(item.Body, "task", &proposed); err != nil {
			return result, err
		}
		if err = e.config.validateTask(proposed, goal); err != nil {
			return result, err
		}
	}
	publication := Publication{Issue: event.Issue, Kind: "progress"}
	evidence, _ := json.MarshalIndent(map[string]any{"event": event, "result": result, "workspace": workspace, "tree": tree, "task_revision": taskHash, "checks": checks}, "", "  ")
	publication.Body = "Reed evidence\n\n```json\n" + string(evidence) + "\n```"
	if result.Task != nil {
		publication.Kind = "create"
		publication.Task = result.Task
		if event.Issue > 0 {
			for _, r := range e.Snapshot().Runs {
				if r.Event.Issue == event.Issue && r.Workspace != "" {
					return result, errors.New("cannot revise acceptance after task execution started")
				}
			}
			publication.Kind = "update"
		}
	} else if event.Kind == "evaluate" || event.Issue == 0 {
		publication.Kind = "evaluation"
	} else if event.Kind == "review" && result.Verdict == "pass" {
		publication.Kind = "complete"
	}
	return e.stage(ctx, event, approved, result, publication)
}
