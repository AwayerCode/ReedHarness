package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fixture keeps GitHub as the only source of task definitions and states.
// Git operations, checkout modifications and check commands use a real temporary repository.
type githubFixture struct {
	mu                                               sync.Mutex
	goal                                             string
	issues                                           map[int]map[string]any
	comments                                         map[int][]json.RawMessage
	members                                          map[int]bool
	states                                           map[int]string
	evaluations                                      []json.RawMessage
	next                                             int
	loseComment                                      bool
	failCommentBeforeWrite                           bool
	createCalls, agentCalls, checkCalls, effectCalls int
	badScope                                         bool
	badReview                                        bool
	failedEffects                                    bool
}

func encoded(v any) ([]byte, int, error) { b, e := json.Marshal(v); return b, 0, e }
func (f *githubFixture) command(ctx context.Context, p string, a []string, dir string, input []byte) ([]byte, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p == "agent" {
		if len(a) > 0 {
			return []byte(`{"protocol":"reed-agent-v1","enforces_scope":true}`), 0, nil
		}
		f.agentCalls++
		var request struct {
			Event    Event         `json:"event"`
			Root     string        `json:"root"`
			ReadOnly bool          `json:"read_only"`
			Paths    []string      `json:"allowed_paths"`
			Checks   []CheckResult `json:"checks"`
			Effects  []CheckResult `json:"effect_results"`
		}
		if err := json.Unmarshal(input, &request); err != nil {
			return nil, 2, err
		}
		result := Decision{Protocol: "reed-agent-v1", Status: "continue", Summary: "observed", Session: request.Event.ID, Evidence: []string{"independent acceptance evidence"}}
		switch request.Event.Kind {
		case "plan":
			paths := []string{"src"}
			if f.badScope {
				paths = []string{"../outside"}
			}
			result.Task = &TaskContract{Title: "Write accepted result", Body: "Return ok", Acceptance: []string{"result.txt equals ok"}, AllowedPaths: paths, Checks: []string{"unit"}}
			result.Next = &Event{Kind: "execute"}
		case "execute":
			if request.ReadOnly || len(request.Paths) != 1 || request.Paths[0] != "src" {
				return nil, 2, errors.New("incorrect execution permissions")
			}
			if err := os.WriteFile(filepath.Join(request.Root, "src/result.txt"), []byte("ok"), 0600); err != nil {
				return nil, 2, err
			}
			result.Next = &Event{Kind: "check", Issue: request.Event.Issue}
		case "review":
			if !request.ReadOnly || len(request.Paths) != 0 || !checksPassed(request.Checks) {
				return nil, 2, errors.New("review not isolated from execution")
			}
			result.Status = "wait"
			result.Verdict = "pass"
			if f.badReview {
				result.Verdict = "fail"
				f.badReview = false
			}
		case "evaluate":
			if len(request.Effects) == 0 {
				return nil, 2, errors.New("missing fresh effects")
			}
			result.Status = "done"
		}
		return encoded(result)
	}
	if p == "unit" {
		f.checkCalls++
		b, err := os.ReadFile(filepath.Join(dir, "src/result.txt"))
		if err != nil || string(b) != "ok" {
			return []byte("failed"), 1, errors.New("wrong delivery")
		}
		return []byte("actual delivery equals ok"), 0, nil
	}
	if p == "effect" {
		f.effectCalls++
		if f.failedEffects {
			return []byte("goal effect absent"), 1, errors.New("effect absent")
		}
		return []byte("fresh effect observed"), 0, nil
	}
	if p != "gh" {
		return nil, 2, fmt.Errorf("unexpected program %s", p)
	}
	flag := func(key string) string {
		for i := range a {
			if a[i] == key && i+1 < len(a) {
				return a[i+1]
			}
		}
		return ""
	}
	if a[0] == "project" {
		switch a[1] {
		case "view":
			return encoded(map[string]string{"id": "P1", "title": "Goal", "readme": f.goal})
		case "item-list":
			items := []json.RawMessage{}
			for n := range f.members {
				b, _ := json.Marshal(map[string]any{"id": "I" + strconv.Itoa(n), "content": map[string]any{"number": n, "title": f.issues[n]["title"], "body": f.issues[n]["body"], "url": fmt.Sprintf("https://github.com/a/repo/issues/%d", n)}, "status": f.states[n]})
				items = append(items, b)
			}
			items = append(items, f.evaluations...)
			return encoded(map[string]any{"items": items, "totalCount": len(items)})
		case "item-add":
			n, _ := strconv.Atoi(filepath.Base(flag("--url")))
			f.members[n] = true
			return encoded(map[string]string{"id": "I" + strconv.Itoa(n)})
		case "field-list":
			return []byte(`{"fields":[{"id":"F","name":"Status","options":[{"id":"ready","name":"Todo"},{"id":"active","name":"In Progress"},{"id":"done","name":"Done"}]}]}`), 0, nil
		case "item-edit":
			n, _ := strconv.Atoi(strings.TrimPrefix(flag("--id"), "I"))
			f.states[n] = flag("--single-select-option-id")
			return encoded(map[string]bool{"ok": true})
		case "item-create":
			b, _ := json.Marshal(map[string]any{"id": "evaluation", "content": map[string]string{"body": flag("--body")}})
			f.evaluations = append(f.evaluations, b)
			return b, 0, nil
		}
	}
	if a[0] == "issue" && a[1] == "view" {
		n, _ := strconv.Atoi(a[2])
		return encoded(f.issues[n])
	}
	if a[0] == "api" {
		path := strings.Split(a[1], "?")[0]
		method := flag("--method")
		if method == "" {
			method = "GET"
		}
		if method == "GET" {
			var all []json.RawMessage
			if strings.HasSuffix(path, "comments") {
				parts := strings.Split(path, "/")
				n, _ := strconv.Atoi(parts[len(parts)-2])
				all = f.comments[n]
			} else {
				for _, v := range f.issues {
					b, _ := json.Marshal(v)
					all = append(all, b)
				}
			}
			if all == nil {
				all = []json.RawMessage{}
			}
			return encoded([][]json.RawMessage{all})
		}
		var v map[string]any
		if err := json.Unmarshal(input, &v); err != nil {
			return nil, 2, err
		}
		if method == "POST" && strings.HasSuffix(path, "/issues") {
			f.next++
			f.createCalls++
			v["number"] = f.next
			v["state"] = "OPEN"
			v["html_url"] = fmt.Sprintf("https://github.com/a/repo/issues/%d", f.next)
			f.issues[f.next] = v
			return encoded(v)
		}
		if method == "POST" && strings.HasSuffix(path, "/comments") {
			if f.failCommentBeforeWrite {
				f.failCommentBeforeWrite = false
				return nil, 1, errors.New("connection lost before write")
			}
			parts := strings.Split(path, "/")
			n, _ := strconv.Atoi(parts[len(parts)-2])
			b, _ := json.Marshal(v)
			f.comments[n] = append(f.comments[n], b)
			if f.loseComment {
				f.loseComment = false
				return nil, 1, errors.New("response lost after write")
			}
			return b, 0, nil
		}
		if method == "PATCH" {
			n, _ := strconv.Atoi(filepath.Base(path))
			for k, value := range v {
				f.issues[n][k] = value
			}
			return encoded(f.issues[n])
		}
	}
	return nil, 2, fmt.Errorf("unexpected GitHub command %v", a)
}
func businessFixture(t *testing.T) (Config, *githubFixture) {
	t.Helper()
	c := testConfig(t)
	c.MaxRuns = 15
	c.RunTimeoutSeconds = 10
	c.Repository = "a/repo"
	c.ProjectOwner = "a"
	c.ProjectNumber = 1
	c.AllowGitHubWrite = true
	c.AllowedPaths = []string{"src"}
	c.CheckCommands = map[string][]string{"unit": {"unit"}, "effect": {"effect"}}
	if err := os.MkdirAll(filepath.Join(c.Root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(c.Root, "src/result.txt"), []byte("before"), 0600)
	for _, a := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture"}} {
		if _, err := git(context.Background(), c.Root, a...); err != nil {
			t.Fatal(err)
		}
	}
	g := GoalContract{AllowedPaths: []string{"src"}, Checks: []string{"unit"}, EffectChecks: []string{"effect"}, MaxRuns: 15, TimeoutSeconds: 10}
	b, _ := json.Marshal(g)
	f := &githubFixture{goal: "Produce the required effect\n```reed-goal\n" + string(b) + "\n```", issues: map[int]map[string]any{}, comments: map[int][]json.RawMessage{}, members: map[int]bool{}, states: map[int]string{}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c, f
}
func startBusiness(t *testing.T, c Config, f *githubFixture) *Engine {
	t.Helper()
	e, err := NewEngine(c, f.command)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.ApproveGoal(context.Background()); err != nil {
		e.Close()
		t.Fatal(err)
	}
	if _, err = e.Trigger(Event{ID: "goal-enabled", Kind: "plan"}); err != nil {
		e.Close()
		t.Fatal(err)
	}
	return e
}
func TestBusinessLoopAcceptance(t *testing.T) {
	c, f := businessFixture(t)
	e := startBusiness(t, c, f)
	defer e.Close()
	s := waitState(t, e, func(s State) bool { return s.Halted || s.Paused })
	if !s.Halted {
		t.Fatalf("loop failed: %+v", s.Runs)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(s.Runs) != 5 || f.createCalls != 1 || f.agentCalls != 4 || f.checkCalls != 1 || f.effectCalls != 1 || f.issues[1]["state"] != "closed" || f.states[1] != "done" || len(f.evaluations) != 1 {
		t.Fatalf("wrong completion: state=%+v fixture=%+v", s, f)
	}
	original, _ := os.ReadFile(filepath.Join(c.Root, "src/result.txt"))
	if string(original) != "before" {
		t.Fatal("execution touched source workspace")
	}
	if s.Runs[1].Workspace == c.Root || s.Runs[2].Tree != s.Runs[3].Tree {
		t.Fatal("checks/review did not bind same isolated deliverable")
	}
}
func TestBusinessLostWriteRecoveryDoesNotReplayAgent(t *testing.T) {
	c, f := businessFixture(t)
	f.loseComment = true
	e := startBusiness(t, c, f)
	s := waitState(t, e, func(s State) bool { return s.Paused })
	r := s.Runs[len(s.Runs)-1]
	if r.Status != "sync_failed" {
		t.Fatalf("%+v", s)
	}
	e.Close()
	var err error
	e, err = NewEngine(c, f.command)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.RetrySync(r.Event.ID, "", false); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, e, func(s State) bool { return s.Paused || s.Halted })
	if !s.Halted {
		t.Fatalf("%+v", s.Runs)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agentCalls != 4 || len(f.comments[1]) != 3 || f.createCalls != 1 {
		t.Fatal("duplicated execution/write", f.agentCalls, len(f.comments[1]), f.createCalls)
	}
}
func TestBusinessUnknownWriteRequiresAbsenceConfirmation(t *testing.T) {
	c, f := businessFixture(t)
	f.failCommentBeforeWrite = true
	e := startBusiness(t, c, f)
	defer e.Close()
	s := waitState(t, e, func(s State) bool { return s.Paused })
	id := s.Runs[len(s.Runs)-1].Event.ID
	if err := e.RetrySync(id, "", false); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, e, func(s State) bool { return s.Paused })
	if !strings.Contains(s.Runs[len(s.Runs)-1].Error, "unconfirmed") {
		t.Fatal(s)
	}
	if err := e.RetrySync(id, "", true); err == nil {
		t.Fatal("absence accepted without evidence")
	}
	if err := e.RetrySync(id, "Verified all comments: previous attempt absent", true); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, e, func(s State) bool { return s.Paused || s.Halted })
	if !s.Halted {
		t.Fatal(s)
	}
}
func TestBusinessAuthorizationAndGoalEvidence(t *testing.T) {
	for _, mode := range []string{"scope", "effects", "review"} {
		t.Run(mode, func(t *testing.T) {
			c, f := businessFixture(t)
			f.badScope = mode == "scope"
			f.failedEffects = mode == "effects"
			f.badReview = mode == "review"
			e := startBusiness(t, c, f)
			defer e.Close()
			s := waitState(t, e, func(s State) bool { return s.Paused || s.Halted })
			f.mu.Lock()
			defer f.mu.Unlock()
			if mode == "scope" {
				if !s.Paused || f.createCalls != 0 {
					t.Fatal("scope escalation created task")
				}
			}
			if mode == "effects" {
				if s.Halted || f.issues[1]["state"] != "closed" || f.effectCalls != 1 {
					t.Fatal("task completion confused with goal completion", s)
				}
			}
			if mode == "review" {
				if !s.Halted || f.checkCalls != 2 || f.agentCalls != 6 {
					t.Fatal("failed review did not repair current task", s)
				}
			}
		})
	}
}

func TestBusinessStaleDeliveryCannotPassReview(t *testing.T) {
	c, f := businessFixture(t)
	var e *Engine
	command := func(ctx context.Context, p string, a []string, d string, b []byte) ([]byte, int, error) {
		out, code, err := f.command(ctx, p, a, d, b)
		if p == "unit" {
			if pauseErr := e.Control("pause"); pauseErr != nil {
				t.Error(pauseErr)
			}
		}
		return out, code, err
	}
	var err error
	e, err = NewEngine(c, command)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.ApproveGoal(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.Trigger(Event{ID: "start", Kind: "plan"})
	s := waitState(t, e, func(s State) bool { return s.Paused && len(s.Runs) == 4 })
	if err = os.WriteFile(filepath.Join(s.Runs[2].Workspace, "src/result.txt"), []byte("tampered after check"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.Control("resume"); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, e, func(s State) bool { return s.Paused })
	if s.Halted || !strings.Contains(s.Runs[3].Error, "missing current") {
		t.Fatal(s)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issues[1]["state"] == "closed" {
		t.Fatal("stale check closed Issue")
	}
}

func TestTaskCannotInvokeUnapprovedCheck(t *testing.T) {
	c, f := businessFixture(t)
	c.CheckCommands["deploy"] = []string{"deploy"}
	var goal GoalContract
	if err := contract(f.goal, "goal", &goal); err != nil {
		t.Fatal(err)
	}
	task := TaskContract{Title: "Unauthorized delivery", Acceptance: []string{"done"}, AllowedPaths: []string{"src"}, Checks: []string{"unit", "deploy"}}
	if err := c.validateTask(task, goal); err == nil {
		t.Fatal("available command treated as goal authorization")
	}
}

func TestCodexLiveBusinessLoop(t *testing.T) {
	if os.Getenv("REED_CODEX_SMOKE") != "1" {
		t.Skip("opt-in live business acceptance")
	}
	c, f := businessFixture(t)
	c.AgentCommand = nil
	c.AgentBackend = "codex"
	c.CodexModel = os.Getenv("REED_CODEX_MODEL")
	c.CodexBaseURL = os.Getenv("REED_CODEX_BASE_URL")
	c.RunTimeoutSeconds = 90
	goal := GoalContract{AllowedPaths: []string{"src"}, Checks: []string{"unit"}, EffectChecks: []string{"effect"}, MaxRuns: 15, TimeoutSeconds: 90}
	b, _ := json.Marshal(goal)
	f.goal = "Goal: deliver an isolated checkout where src/result.txt contains exactly ok (no newline required). Baseline is before. Do not modify the source checkout. Create exactly one task, select the unit check and no Skills. The service effect check independently reads the delivered file from the isolated checkout; passing that fresh measurement is the goal effect. No commit, merge, deployment or network is required.\n```reed-goal\n" + string(b) + "\n```"
	command := func(ctx context.Context, p string, a []string, d string, b []byte) ([]byte, int, error) {
		if p == c.Codex {
			return Execute(ctx, p, a, d, b)
		}
		if p == "effect" {
			f.mu.Lock()
			f.effectCalls++
			f.mu.Unlock()
			file := filepath.Join(c.StateDir, "workspaces", "issue-1", "src", "result.txt")
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "ok" {
				return []byte("fresh observation: delivered file is not ok"), 1, errors.New("goal effect absent")
			}
			return []byte("fresh independent observation: delivered checkout src/result.txt contains exactly ok"), 0, nil
		}
		return f.command(ctx, p, a, d, b)
	}
	e, err := NewEngine(c, command)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.ApproveGoal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Trigger(Event{ID: "live-goal-enabled", Kind: "plan", Input: "Plan the single minimal task needed to meet the goal; use a task proposal."}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(240 * time.Second)
	for time.Now().Before(deadline) {
		s := e.Snapshot()
		if s.Paused {
			t.Fatalf("live loop paused: %+v", s.Runs)
		}
		if s.Halted {
			f.mu.Lock()
			closed := f.issues[1]["state"] == "closed"
			effects := f.effectCalls
			f.mu.Unlock()
			if !closed || effects < 1 || len(s.Runs) < 5 {
				t.Fatal("missing full acceptance", s)
			}
			original, _ := os.ReadFile(filepath.Join(c.Root, "src/result.txt"))
			if string(original) != "before" {
				t.Fatal("source changed")
			}
			t.Log("real Codex planned, executed, independently reviewed and evaluated; GitHub transport simulated, actual checkout/effect observed")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("live business loop timed out", e.Snapshot())
}

func TestPublicationMarkerCannotSubstituteDifferentEvidence(t *testing.T) {
	c, f := businessFixture(t)
	f.loseComment = true
	e := startBusiness(t, c, f)
	defer e.Close()
	s := waitState(t, e, func(s State) bool { return s.Paused })
	id := s.Runs[len(s.Runs)-1].Event.ID
	f.mu.Lock()
	var comment map[string]string
	json.Unmarshal(f.comments[1][0], &comment)
	comment["body"] = "Changed evidence\n" + comment["body"]
	f.comments[1][0], _ = json.Marshal(comment)
	f.mu.Unlock()
	if err := e.RetrySync(id, "", false); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, e, func(s State) bool { return s.Paused })
	if s.Halted || !strings.Contains(s.Runs[len(s.Runs)-1].Error, "conflicts") {
		t.Fatal(s)
	}
}

func TestManagementCannotRewriteOriginalAcceptance(t *testing.T) {
	c, f := businessFixture(t)
	e := startBusiness(t, c, f)
	defer e.Close()
	waitState(t, e, func(s State) bool { return s.Halted || s.Paused })
	if err := e.Edit(context.Background(), GitHubEdit{Kind: "task", Number: 1, Title: "Lower acceptance", Body: "accept anything"}); err == nil {
		t.Fatal("management bypassed immutable acceptance")
	}
}
