package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Issue int    `json:"issue"`
	Input string `json:"input"`
}
type Decision struct {
	Task     *TaskContract `json:"task,omitempty"`
	Verdict  string        `json:"verdict,omitempty"`
	Protocol string        `json:"protocol"`
	Status   string        `json:"status"`
	Summary  string        `json:"summary"`
	Evidence []string      `json:"evidence"`
	Next     *Event        `json:"next,omitempty"`
	Session  string        `json:"session,omitempty"`
}
type Run struct {
	Workspace   string        `json:"workspace,omitempty"`
	TaskHash    string        `json:"task_hash,omitempty"`
	Tree        string        `json:"tree,omitempty"`
	Checks      []CheckResult `json:"checks,omitempty"`
	Publication *Publication  `json:"publication,omitempty"`
	Event       Event         `json:"event"`
	Status      string        `json:"status"`
	Started     time.Time     `json:"started,omitempty"`
	Finished    time.Time     `json:"finished,omitempty"`
	Result      *Decision     `json:"result,omitempty"`
	Error       string        `json:"error,omitempty"`
}
type State struct {
	LastError    string `json:"last_error,omitempty"`
	Binding      string `json:"binding"`
	Paused       bool   `json:"paused"`
	Halted       bool   `json:"halted"`
	GoalBudget   int    `json:"goal_budget"`
	ApprovedGoal string `json:"approved_goal"`
	Runs         []Run  `json:"runs"`
}
type Engine struct {
	mu      sync.Mutex
	config  Config
	command Command
	state   State
	ctx     context.Context
	cancel  context.CancelFunc
	active  context.CancelFunc
	wake    chan struct{}
	done    chan struct{}
	fatal   error
	release func()
}

func NewEngine(c Config, command Command) (*Engine, error) {
	if err := os.MkdirAll(c.StateDir, 0700); err != nil {
		return nil, err
	}
	realState, err := filepath.EvalSymlinks(c.StateDir)
	if err != nil {
		return nil, err
	}
	c.StateDir = realState
	if rel, err := filepath.Rel(c.Root, c.StateDir); err == nil && filepath.IsLocal(rel) {
		return nil, errors.New("state directory resolves inside managed workspace")
	}
	lockPath := filepath.Join(c.StateDir, "service.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("state directory is locked; verify the previous service has stopped before removing service.lock: %w", err)
	}
	fmt.Fprintf(lock, "%d\n", os.Getpid())
	lock.Close()
	release := func() { os.Remove(lockPath) }
	e := &Engine{config: c, command: command, wake: make(chan struct{}, 1), done: make(chan struct{}), release: release, state: State{Runs: []Run{}}}
	data, err := os.ReadFile(filepath.Join(c.StateDir, "runs.json"))
	if err == nil {
		if err = json.Unmarshal(data, &e.state); err != nil {
			release()
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		release()
		return nil, err
	}
	// A state directory cannot silently authorize a different workspace or host.
	bindingJSON, _ := json.Marshal(c)
	binding := sha256.Sum256(bindingJSON)
	fingerprint := hex.EncodeToString(binding[:])
	if e.state.Binding != "" && e.state.Binding != fingerprint {
		release()
		return nil, errors.New("configuration differs from this state directory; use a new authorized state directory")
	}
	e.state.Binding = fingerprint
	// Restart never silently replays a task whose external effects are unknown.
	for i := range e.state.Runs {
		if e.state.Runs[i].Publication != nil && e.state.Runs[i].Status == "publishing" {
			e.state.Runs[i].Status = "sync_failed"
			e.state.Paused = true
		}
		if e.state.Runs[i].Status == "running" {
			e.state.Runs[i].Status = "interrupted"
			e.state.Runs[i].Error = "previous execution outcome must be reconciled"
			e.state.Paused = true
		}
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	if err = e.saveLocked(); err != nil {
		release()
		e.cancel()
		return nil, err
	}
	go e.loop()
	e.signal()
	return e, nil
}
func (e *Engine) Close() {
	e.cancel()
	e.mu.Lock()
	if e.active != nil {
		e.active()
	}
	e.mu.Unlock()
	<-e.done
	e.release()
}
func (e *Engine) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}
func (e *Engine) saveLocked() (err error) {
	defer func() {
		if err != nil {
			e.fatal = err
			e.state.Paused = true
		}
	}()
	data, err := json.MarshalIndent(e.state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(e.config.StateDir, ".runs-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(e.config.StateDir, "runs.json"))
	}
	if err != nil {
		e.fatal = err
		e.state.Paused = true
	}
	return err
}
func (e *Engine) Snapshot() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	data, _ := json.Marshal(e.state)
	var s State
	json.Unmarshal(data, &s)
	return s
}
func validKind(k string) bool {
	return k == "check" || k == "plan" || k == "execute" || k == "review" || k == "evaluate"
}
func (e *Engine) Trigger(v Event) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.triggerLocked(v, true)
}
func (e *Engine) triggerLocked(v Event, persist bool) (bool, error) {
	if v.ID == "" || len(v.ID) > 200 || !validKind(v.Kind) || v.Issue < 0 {
		return false, errors.New("event requires id, plan/execute/review/evaluate kind and nonnegative issue")
	}
	if e.fatal != nil {
		return false, e.fatal
	}
	if e.state.Halted {
		return false, errors.New("goal is stopped")
	}
	for _, r := range e.state.Runs {
		if r.Event.ID == v.ID {
			return false, nil
		}
		if (r.Status == "queued" || r.Status == "running") && r.Event.Issue == v.Issue {
			return false, errors.New("work for this task is already queued or running")
		}
	}
	limit := e.config.MaxRuns
	if e.state.GoalBudget > 0 && e.state.GoalBudget < limit {
		limit = e.state.GoalBudget
	}
	if len(e.state.Runs) >= limit {
		return false, errors.New("run budget exhausted")
	}
	e.state.Runs = append(e.state.Runs, Run{Event: v, Status: "queued"})
	if persist {
		if err := e.saveLocked(); err != nil {
			return false, err
		}
	}
	e.signal()
	return true, nil
}
func (e *Engine) Control(action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fatal != nil {
		return e.fatal
	}
	switch action {
	case "pause":
		e.state.Paused = true
	case "resume":
		if e.state.Halted {
			return errors.New("goal is stopped; create a newly authorized run configuration")
		}
		for _, r := range e.state.Runs {
			if r.Status == "interrupted" || r.Status == "sync_failed" {
				return errors.New("reconcile interrupted runs before resuming")
			}
		}
		e.state.Paused = false
	case "stop":
		e.state.Halted = true
		e.state.Paused = true
		if e.active != nil {
			e.active()
		}
		for i := range e.state.Runs {
			if e.state.Runs[i].Status == "queued" {
				e.state.Runs[i].Status = "cancelled"
			}
		}
	default:
		return errors.New("unknown run action")
	}
	if err := e.saveLocked(); err != nil {
		return err
	}
	e.signal()
	return nil
}
func (e *Engine) Reconcile(id, note string) error {
	if note == "" {
		return errors.New("reconciliation evidence is required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.state.Runs {
		r := &e.state.Runs[i]
		if r.Event.ID == id && r.Status == "interrupted" {
			r.Status = "reconciled"
			r.Error = note
			return e.saveLocked()
		}
	}
	return errors.New("interrupted run not found")
}
func goalDigest(raw []byte) (string, error) {
	var p struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Readme string `json:"readme"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	if p.ID == "" {
		return "", errors.New("Project identity missing")
	}
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func (e *Engine) ApproveGoal(ctx context.Context) error {
	raw, err := (GitHub{e.config, e.command}).Project(ctx)
	if err != nil {
		return err
	}
	var project struct {
		Readme string `json:"readme"`
	}
	if err = json.Unmarshal(raw, &project); err != nil {
		return err
	}
	var goal GoalContract
	if err = contract(project.Readme, "goal", &goal); err != nil {
		return err
	}
	if err = e.config.validateGoal(goal); err != nil {
		return err
	}
	hash, err := goalDigest(raw)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active != nil {
		return errors.New("pause and wait for the active run before approving a goal change")
	}
	e.state.GoalBudget = goal.MaxRuns
	e.state.ApprovedGoal = hash
	return e.saveLocked()
}
func (e *Engine) loop() {
	defer close(e.done)
	var tick <-chan time.Time
	var ticker *time.Ticker
	if e.config.IntervalSeconds > 0 {
		ticker = time.NewTicker(time.Duration(e.config.IntervalSeconds) * time.Second)
		tick = ticker.C
		defer ticker.Stop()
	}
	for {
		select {
		case <-e.ctx.Done():
			return
		case t := <-tick:
			s := e.Snapshot()
			if !s.Paused && !s.Halted {
				if _, err := e.Trigger(Event{ID: fmt.Sprintf("timer:%d", t.Unix()), Kind: "evaluate"}); err != nil {
					e.mu.Lock()
					e.state.Paused = true
					e.state.LastError = err.Error()
					e.saveLocked()
					e.mu.Unlock()
				}
			}
		case <-e.wake:
		}
		for e.takeAndRun() {
		}
	}
}
func (e *Engine) takeAndRun() bool {
	e.mu.Lock()
	if e.ctx.Err() != nil || e.fatal != nil || e.state.Paused || e.state.Halted {
		e.mu.Unlock()
		return false
	}
	index := -1
	for i, r := range e.state.Runs {
		if r.Status == "queued" || r.Status == "publishing" {
			index = i
			break
		}
	}
	if index < 0 {
		e.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.config.timeout())
	e.active = cancel
	publishing := e.state.Runs[index].Status == "publishing"
	if !publishing {
		e.state.Runs[index].Status = "running"
	}
	e.state.Runs[index].Started = time.Now()
	event := e.state.Runs[index].Event
	approved := e.state.ApprovedGoal
	if err := e.saveLocked(); err != nil {
		cancel()
		e.active = nil
		e.mu.Unlock()
		return false
	}
	e.mu.Unlock()
	var result Decision
	var err error
	if publishing {
		result, err = e.publish(ctx, event, approved)
	} else if e.config.ProjectNumber > 0 {
		result, err = e.runBusiness(ctx, event, approved)
	} else {
		result, err = e.run(ctx, event, approved)
	}
	if ctx.Err() != nil && err == nil {
		err = ctx.Err()
	}
	interrupted := ctx.Err() != nil || errors.Is(err, errUnknownEffects)
	cancel()
	e.mu.Lock()
	e.active = nil
	r := &e.state.Runs[index]
	r.Finished = time.Now()
	if err != nil {
		r.Status = "failed"
		r.Error = err.Error()
		e.state.Paused = true
		if r.Publication != nil {
			r.Status = "sync_failed"
		} else if interrupted {
			r.Status = "interrupted"
		}
	} else {
		r.Status = result.Status
		r.Result = &result
		if result.Status == "needs_input" {
			e.state.Paused = true
		}
		if result.Status == "done" {
			e.state.Halted = true
			for i := range e.state.Runs {
				if e.state.Runs[i].Status == "queued" {
					e.state.Runs[i].Status = "cancelled"
				}
			}
		}
	}
	// Persist the result and its follow-up event together: a restart cannot lose the continuation.
	if err == nil && result.Next != nil && !e.state.Halted {
		next := *result.Next
		digest := sha256.Sum256([]byte(event.ID))
		next.ID = "next:" + hex.EncodeToString(digest[:])
		if _, triggerErr := e.triggerLocked(next, false); triggerErr != nil {
			e.state.Paused = true
			e.state.Runs[index].Error = "next event not scheduled: " + triggerErr.Error()
		}
	}
	e.saveLocked()
	e.mu.Unlock()
	return true
}

var errUnknownEffects = errors.New("Agent outcome requires reconciliation")

func (e *Engine) run(ctx context.Context, event Event, approved string) (result Decision, runErr error) {
	invoked := false
	defer func() {
		if invoked && runErr != nil {
			runErr = fmt.Errorf("%w: %v", errUnknownEffects, runErr)
		}
	}()
	if len(e.config.AgentCommand) == 0 && e.config.AgentBackend == "" {
		return result, errors.New("no Agent host adapter configured")
	}
	var goal, task json.RawMessage
	if e.config.ProjectNumber > 0 {
		var err error
		g := GitHub{e.config, e.command}
		goal, err = g.Project(ctx)
		if err != nil {
			return result, err
		}
		hash, err := goalDigest(goal)
		if err != nil {
			return result, err
		}
		if approved == "" || hash != approved {
			return result, errors.New("goal definition needs approval")
		}
		if event.Issue > 0 {
			task, err = g.Issue(ctx, event.Issue)
		} else {
			task, err = g.Tasks(ctx)
		}
		if err != nil {
			return result, err
		}
	}
	skillList, err := ListSkills(e.config.SkillsDir, e.config.SkillVersion)
	if err != nil {
		return result, err
	}
	request := map[string]any{"history": e.Snapshot().Runs, "protocol": "reed-agent-v1", "event": event, "goal": goal, "task": task, "root": e.config.Root, "allowed_paths": e.config.AllowedPaths, "skills": skillList, "swe": e.config.SWE, "timeout_seconds": e.config.RunTimeoutSeconds, "fresh_session": true, "read_only": event.Kind != "execute", "github_write": false}
	invoked = true
	result, err = e.invokeAgent(ctx, request)
	if err != nil {
		return result, err
	}
	if e.config.ProjectNumber > 0 {
		latest, checkErr := (GitHub{e.config, e.command}).Project(ctx)
		if checkErr != nil {
			return result, checkErr
		}
		hash, checkErr := goalDigest(latest)
		if checkErr != nil || hash != approved {
			return result, errors.New("goal changed during execution; review results before continuing")
		}
	}
	if result.Protocol != "reed-agent-v1" || result.Summary == "" {
		return result, errors.New("invalid Agent decision")
	}
	if event.Kind == "review" {
		if result.Session == "" {
			return result, errors.New("review requires an independent session identity")
		}
		for _, previous := range e.Snapshot().Runs {
			if previous.Result != nil && previous.Result.Session == result.Session {
				return result, errors.New("review reused an earlier Agent session")
			}
		}
	}
	switch result.Status {
	case "continue":
		if result.Next == nil || !validKind(result.Next.Kind) {
			return result, errors.New("continue requires a valid next event")
		}
	case "wait", "needs_input":
		if result.Next != nil {
			return result, errors.New("waiting decision cannot start more work")
		}
	case "done":
		if event.Kind != "evaluate" || len(result.Evidence) == 0 || result.Next != nil {
			return result, errors.New("goal completion requires an evaluation with effect evidence")
		}
	default:
		return result, errors.New("unknown Agent decision status")
	}
	if result.Next != nil && result.Next.Issue != event.Issue {
		return result, errors.New("Agent cannot switch the authorized task through a continuation")
	}
	return result, nil
}
