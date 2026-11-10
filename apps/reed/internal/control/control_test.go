package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	c := Config{Root: t.TempDir(), StateDir: t.TempDir(), AgentCommand: []string{"agent"}, MaxRuns: 4, RunTimeoutSeconds: 2}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}
func capable(ctx context.Context, p string, args []string, dir string, input []byte) ([]byte, int, error) {
	if len(args) > 0 && args[len(args)-1] == "--capabilities" {
		return []byte(`{"protocol":"reed-agent-v1","enforces_scope":true}`), 0, nil
	}
	var req struct {
		Event Event `json:"event"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, 2, err
	}
	result := Decision{Protocol: "reed-agent-v1", Summary: "verified", Session: req.Event.ID, Status: "done", Evidence: []string{"effect measurement"}}
	if req.Event.Kind != "evaluate" {
		result.Status = "continue"
		result.Next = &Event{Kind: "evaluate", Issue: req.Event.Issue}
	}
	data, _ := json.Marshal(result)
	return data, 0, nil
}
func waitState(t *testing.T, e *Engine, predicate func(State) bool) State {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		s := e.Snapshot()
		if predicate(s) {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out: %+v", e.Snapshot())
	return State{}
}
func TestEventDecisionAndGoalEvaluation(t *testing.T) {
	c := testConfig(t)
	e, err := NewEngine(c, capable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if ok, err := e.Trigger(Event{ID: "delivery", Kind: "review", Issue: 4}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	s := waitState(t, e, func(s State) bool { return s.Halted })
	if len(s.Runs) != 2 || s.Runs[0].Result.Next.Kind != "evaluate" || s.Runs[1].Status != "done" {
		t.Fatal(s)
	}
	if _, err = e.Trigger(Event{ID: "after-stop", Kind: "execute"}); err == nil {
		t.Fatal("stopped goal accepted work")
	}
}
func TestDedupPauseAndBudget(t *testing.T) {
	c := testConfig(t)
	c.MaxRuns = 1
	entered := make(chan struct{})
	release := make(chan struct{})
	cmd := func(ctx context.Context, p string, a []string, d string, b []byte) ([]byte, int, error) {
		if len(a) > 0 {
			return capable(ctx, p, a, d, b)
		}
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, 2, ctx.Err()
		}
		return []byte(`{"protocol":"reed-agent-v1","status":"wait","summary":"waiting"}`), 0, nil
	}
	e, err := NewEngine(c, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.Control("pause")
	e.Trigger(Event{ID: "same", Kind: "execute", Issue: 7})
	if ok, err := e.Trigger(Event{ID: "same", Kind: "execute", Issue: 7}); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err := e.Trigger(Event{ID: "other", Kind: "execute", Issue: 7}); err == nil {
		t.Fatal("duplicate task queued")
	}
	e.Control("resume")
	<-entered
	if _, err := e.Trigger(Event{ID: "over-budget", Kind: "execute", Issue: 8}); err == nil {
		t.Fatal("budget exceeded")
	}
	close(release)
	waitState(t, e, func(s State) bool { return s.Runs[0].Status == "wait" })
}
func TestStopAndRecoveryRequireReconciliation(t *testing.T) {
	c := testConfig(t)
	started := make(chan struct{})
	cmd := func(ctx context.Context, p string, a []string, d string, b []byte) ([]byte, int, error) {
		if len(a) > 0 {
			return capable(ctx, p, a, d, b)
		}
		close(started)
		<-ctx.Done()
		return nil, 2, ctx.Err()
	}
	e, err := NewEngine(c, cmd)
	if err != nil {
		t.Fatal(err)
	}
	e.Trigger(Event{ID: "attempt", Kind: "execute"})
	<-started
	e.Close()
	e, err = NewEngine(c, capable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	s := e.Snapshot()
	if s.Runs[0].Status != "interrupted" || !s.Paused {
		t.Fatal(s)
	}
	if err = e.Control("resume"); err == nil {
		t.Fatal("replayed unknown effects")
	}
	if err = e.Reconcile("attempt", "confirmed old host stopped, deliverable exists"); err != nil {
		t.Fatal(err)
	}
	if err = e.Control("resume"); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot().Runs) != 1 {
		t.Fatal("reconciliation repeated execution")
	}
}
func TestStateDirectoryExclusive(t *testing.T) {
	c := testConfig(t)
	e, err := NewEngine(c, capable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if other, err := NewEngine(c, capable); err == nil {
		other.Close()
		t.Fatal("two owners of state")
	}
}
func TestScopeCapabilityRequired(t *testing.T) {
	c := testConfig(t)
	calls := 0
	cmd := func(context.Context, string, []string, string, []byte) ([]byte, int, error) {
		calls++
		return []byte(`{"protocol":"reed-agent-v1","enforces_scope":false}`), 0, nil
	}
	e, err := NewEngine(c, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.Trigger(Event{ID: "x", Kind: "execute"})
	waitState(t, e, func(s State) bool { return s.Paused })
	if calls != 1 {
		t.Fatal("Agent execution started without enforcing host")
	}
}
func TestGoalApprovalAndChanges(t *testing.T) {
	c := testConfig(t)
	c.ProjectOwner = "owner"
	c.ProjectNumber = 1
	c.Repository = "owner/repo"
	var mu sync.Mutex
	goalJSON, _ := json.Marshal(GoalContract{AllowedPaths: []string{}, EffectChecks: []string{"effect"}, MaxRuns: 4, TimeoutSeconds: 2})
	c.CheckCommands = map[string][]string{"effect": {"effect"}}
	readme := "goal one\n```reed-goal\n" + string(goalJSON) + "\n```"
	cmd := func(ctx context.Context, p string, a []string, d string, b []byte) ([]byte, int, error) {
		if p == c.GH {
			if a[1] == "view" {
				mu.Lock()
				defer mu.Unlock()
				data, _ := json.Marshal(map[string]string{"id": "P1", "title": "Goal", "readme": readme})
				return data, 0, nil
			}
			return []byte(`{"items":[],"totalCount":0}`), 0, nil
		}
		return capable(ctx, p, a, d, b)
	}
	e, err := NewEngine(c, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.ApproveGoal(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	readme = "changed"
	mu.Unlock()
	e.Trigger(Event{ID: "changed", Kind: "evaluate"})
	s := waitState(t, e, func(s State) bool { return s.Paused })
	if !strings.Contains(s.Runs[0].Error, "approval") {
		t.Fatal(s)
	}
}
func TestTimerTriggersEvaluation(t *testing.T) {
	c := testConfig(t)
	c.IntervalSeconds = 1
	e, err := NewEngine(c, capable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	s := waitState(t, e, func(s State) bool { return s.Halted })
	if len(s.Runs) != 1 || s.Runs[0].Event.Kind != "evaluate" {
		t.Fatal(s)
	}
}
func TestTaskCannotDeclareGoalDone(t *testing.T) {
	c := testConfig(t)
	cmd := func(ctx context.Context, p string, a []string, d string, b []byte) ([]byte, int, error) {
		if len(a) > 0 {
			return capable(ctx, p, a, d, b)
		}
		return []byte(`{"protocol":"reed-agent-v1","status":"done","summary":"tests passed","evidence":["tests"]}`), 0, nil
	}
	e, err := NewEngine(c, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.Trigger(Event{ID: "task", Kind: "execute"})
	s := waitState(t, e, func(s State) bool { return s.Paused })
	if s.Halted || !strings.Contains(s.Runs[0].Error, "evaluation") {
		t.Fatal(s)
	}
}

func TestHTTPBoundariesAndSkills(t *testing.T) {
	c := testConfig(t)
	c.SkillsDir = t.TempDir()
	os.Mkdir(filepath.Join(c.SkillsDir, "sample"), 0700)
	os.WriteFile(filepath.Join(c.SkillsDir, "sample", "SKILL.md"), []byte("# Sample"), 0600)
	s, err := NewServer(c, capable, "127.0.0.1:47832")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	request := func(method, path, host, origin, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		r.Host = host
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Reed-Token", token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	for _, v := range [][2]string{{"evil.example", ""}, {s.host, "http://evil.example"}} {
		if w := request("GET", "/api/state", v[0], v[1], "", ""); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if w := request("POST", "/api/control", s.host, "", "", `{"action":"pause"}`); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/api/control", s.host, "", s.token, `{"action":"pause"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("GET", "/api/skills", s.host, "", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Sample") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/api/edit", s.host, "", s.token, `{"kind":"goal","title":"x"}`); w.Code == 200 {
		t.Fatal("writes enabled by HTTP")
	}
	if w := request("GET", "/", s.host, "", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Business Harness") {
		t.Fatal(w.Code)
	}
}
func TestSkillAndDesignEscape(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "skill"), 0700)
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "skill", "escape.md")); err != nil {
		t.Skip(err)
	}
	if _, err := ReadSkill(root, "skill", "escape.md"); err == nil {
		t.Fatal("symlink escaped")
	}
	if _, err := ReadSkill(root, "skill", "../secret"); err == nil {
		t.Fatal("path escaped")
	}
	c := testConfig(t)
	s := &Server{config: c}
	if _, err := s.designPath("../system-design.yaml"); err == nil {
		t.Fatal("design escaped")
	}
}
func TestSWEVersionAndExitCodes(t *testing.T) {
	c := testConfig(t)
	c.SWEVersion = "1.2.3"
	for _, code := range []int{1, 2} {
		s := &Server{config: c, command: func(_ context.Context, _ string, a []string, _ string, _ []byte) ([]byte, int, error) {
			if a[0] == "version" {
				return []byte(`{"version":"1.2.3"}`), 0, nil
			}
			return []byte(`{"diagnostics":["invalid"]}`), code, errors.New("exit")
		}}
		if _, err := s.swe(context.Background(), "render", "system-design.yaml"); err == nil {
			t.Fatal("failed check treated as HTML")
		}
	}
}
func TestGitHubScopeAndWriteGate(t *testing.T) {
	c := testConfig(t)
	c.Repository = "a/repo"
	c.ProjectOwner = "a"
	c.ProjectNumber = 1
	var calls []string
	cmd := func(_ context.Context, _ string, a []string, _ string, _ []byte) ([]byte, int, error) {
		calls = append(calls, strings.Join(a, " "))
		if a[0] == "project" {
			return []byte(`{"totalCount":2,"items":[{"content":{"url":"https://github.com/a/repo/issues/1"}},{"content":{"url":"https://github.com/b/repo/issues/2"}}]}`), 0, nil
		}
		return []byte(`{"number":1}`), 0, nil
	}
	g := GitHub{c, cmd}
	data, err := g.Tasks(context.Background())
	if err != nil || strings.Contains(string(data), "b/repo") {
		t.Fatal(string(data), err)
	}
	if _, err = g.Issue(context.Background(), 2); err == nil {
		t.Fatal("cross-project issue read")
	}
	if err = g.Edit(context.Background(), GitHubEdit{Kind: "task", Number: 1}); err == nil {
		t.Fatal("write gate bypassed")
	}
	for _, call := range calls {
		if strings.Contains(call, " edit ") {
			t.Fatal(call)
		}
	}
}
func ExampleDecision() {
	fmt.Println(`{"protocol":"reed-agent-v1","status":"wait","summary":"observe effects at next interval"}`)
	// Output: {"protocol":"reed-agent-v1","status":"wait","summary":"observe effects at next interval"}
}

var _ http.Handler = (*Server)(nil)

func TestConfigurationCannotReuseOldAuthorization(t *testing.T) {
	c := testConfig(t)
	e, err := NewEngine(c, capable)
	if err != nil {
		t.Fatal(err)
	}
	e.Close()
	c.Root = t.TempDir()
	if e, err = NewEngine(c, capable); err == nil {
		e.Close()
		t.Fatal("different workspace reused old authorization")
	}
}

func TestAgentFailureMustBeReconciled(t *testing.T) {
	c := testConfig(t)
	cmd := func(ctx context.Context, p string, args []string, dir string, input []byte) ([]byte, int, error) {
		if len(args) > 0 {
			return capable(ctx, p, args, dir, input)
		}
		return nil, 1, errors.New("process exited after creating a deliverable")
	}
	e, err := NewEngine(c, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.Trigger(Event{ID: "unknown-effects", Kind: "execute"})
	s := waitState(t, e, func(s State) bool { return s.Paused })
	if s.Runs[0].Status != "interrupted" {
		t.Fatal(s)
	}
	if err := e.Control("resume"); err == nil {
		t.Fatal("unverified side effects resumed")
	}
}

func TestReviewCannotReuseExecutionSession(t *testing.T) {
	c := testConfig(t)
	cmd := func(ctx context.Context, p string, args []string, dir string, input []byte) ([]byte, int, error) {
		if len(args) > 0 {
			return capable(ctx, p, args, dir, input)
		}
		var req struct {
			Event Event `json:"event"`
		}
		json.Unmarshal(input, &req)
		result := Decision{Protocol: "reed-agent-v1", Status: "wait", Summary: "checked", Session: "same-session"}
		if req.Event.Kind == "execute" {
			result.Status = "continue"
			result.Next = &Event{Kind: "review"}
		}
		data, _ := json.Marshal(result)
		return data, 0, nil
	}
	e, err := NewEngine(c, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.Trigger(Event{ID: "execute", Kind: "execute"})
	s := waitState(t, e, func(s State) bool { return s.Paused })
	if len(s.Runs) != 2 || !strings.Contains(s.Runs[1].Error, "reused") {
		t.Fatal(s)
	}
}

func TestResultAndContinuationSurviveRestart(t *testing.T) {
	c := testConfig(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	cmd := func(ctx context.Context, p string, a []string, d string, b []byte) ([]byte, int, error) {
		if len(a) > 0 {
			return capable(ctx, p, a, d, b)
		}
		close(entered)
		<-release
		return []byte(`{"protocol":"reed-agent-v1","status":"continue","summary":"ready","next":{"kind":"evaluate","issue":0}}`), 0, nil
	}
	e, err := NewEngine(c, cmd)
	if err != nil {
		t.Fatal(err)
	}
	e.Trigger(Event{ID: "first", Kind: "plan"})
	<-entered
	e.Control("pause")
	close(release)
	waitState(t, e, func(s State) bool { return len(s.Runs) == 2 })
	e.Close()
	e, err = NewEngine(c, capable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if s := e.Snapshot(); !s.Paused || len(s.Runs) != 2 || s.Runs[1].Status != "queued" {
		t.Fatal(s)
	}
	e.Control("resume")
	s := waitState(t, e, func(s State) bool { return s.Halted })
	if len(s.Runs) != 2 {
		t.Fatal("repeated completed task", s)
	}
}
