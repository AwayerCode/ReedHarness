package control

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var web embed.FS

type Server struct {
	config  Config
	command Command
	engine  *Engine
	host    string
	token   string
}

func NewServer(c Config, command Command, host string) (*Server, error) {
	e, err := NewEngine(c, command)
	if err != nil {
		return nil, err
	}
	var secret [32]byte
	if _, err = rand.Read(secret[:]); err != nil {
		e.Close()
		return nil, err
	}
	return &Server{c, command, e, host, hex.EncodeToString(secret[:])}, nil
}
func (s *Server) Close() { s.engine.Close() }
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'")
	origin := "http://" + s.host
	if r.Host != s.host || r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "local same-origin access required", 403)
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if r.Method == "POST" && r.Header.Get("X-Reed-Token") != s.token {
		http.Error(w, "invalid management token", 403)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var value any
	var err error
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/favicon.ico":
			w.WriteHeader(http.StatusNoContent)
			return
		case "/", "/app.js", "/style.css":
			file := "index.html"
			mime := "text/html; charset=utf-8"
			if r.URL.Path == "/app.js" {
				file = "app.js"
				mime = "text/javascript; charset=utf-8"
			}
			if r.URL.Path == "/style.css" {
				file = "style.css"
				mime = "text/css; charset=utf-8"
			}
			data, _ := web.ReadFile("web/" + file)
			w.Header().Set("Content-Type", mime)
			w.Write(data)
			return
		case "/api/state":
			value = map[string]any{"service": "reed-control", "token": s.token, "state": s.engine.Snapshot(), "root": s.config.Root, "repository": s.config.Repository, "project_owner": s.config.ProjectOwner, "project_number": s.config.ProjectNumber, "github_write": s.config.AllowGitHubWrite, "agent_configured": len(s.config.AgentCommand) > 0 || s.config.AgentBackend != ""}
		case "/api/goal":
			value, err = (GitHub{s.config, s.command}).Project(ctx)
		case "/api/tasks":
			value, err = (GitHub{s.config, s.command}).Tasks(ctx)
		case "/api/task":
			var n int
			n, err = strconv.Atoi(r.URL.Query().Get("number"))
			if err == nil {
				value, err = (GitHub{s.config, s.command}).Issue(ctx, n)
			}
		case "/api/skills":
			value, err = ListSkills(s.config.SkillsDir, s.config.SkillVersion)
		case "/api/skill":
			var text string
			text, err = ReadSkill(s.config.SkillsDir, r.URL.Query().Get("name"), r.URL.Query().Get("file"))
			value = map[string]string{"content": text}
		case "/api/designs":
			var data []byte
			data, err = s.swe(ctx, "list", "")
			if err == nil {
				if !json.Valid(data) {
					err = errors.New("swe returned invalid discovery JSON")
				} else {
					value = json.RawMessage(data)
				}
			}
		case "/architecture":
			var file string
			file, err = s.designPath(r.URL.Query().Get("file"))
			if err == nil {
				var data []byte
				data, err = s.swe(ctx, "render", file)
				if err == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'self'")
					w.Write(data)
					return
				}
			}
		default:
			http.NotFound(w, r)
			return
		}
	} else {
		switch r.URL.Path {
		case "/api/event":
			var v Event
			err = decode(w, r, &v)
			if err == nil {
				var added bool
				added, err = s.engine.Trigger(v)
				value = map[string]bool{"queued": added}
			}
		case "/api/control":
			var v struct {
				Action          string `json:"action"`
				ID              string `json:"id"`
				Evidence        string `json:"evidence"`
				ConfirmedAbsent bool   `json:"confirmed_absent"`
			}
			err = decode(w, r, &v)
			if err == nil {
				if v.Action == "approve_goal" {
					err = s.engine.ApproveGoal(ctx)
				} else if v.Action == "retry_sync" {
					err = s.engine.RetrySync(v.ID, v.Evidence, v.ConfirmedAbsent)
				} else if v.Action == "reconcile" {
					err = s.engine.Reconcile(v.ID, v.Evidence)
				} else {
					err = s.engine.Control(v.Action)
				}
			}
			value = map[string]bool{"ok": err == nil}
		case "/api/edit":
			var v GitHubEdit
			err = decode(w, r, &v)
			if err == nil {
				err = s.engine.Edit(ctx, v)
			}
			value = map[string]bool{"ok": err == nil}
		default:
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(400)
		value = map[string]string{"error": err.Error()}
	}
	json.NewEncoder(w).Encode(value)
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("application/json required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("single JSON request required")
	}
	return nil
}
func (s *Server) designPath(file string) (string, error) {
	if !filepath.IsLocal(file) {
		return "", errors.New("design must be workspace-relative")
	}
	real, err := filepath.EvalSymlinks(filepath.Join(s.config.Root, file))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(s.config.Root, real)
	if err != nil || !filepath.IsLocal(rel) || filepath.Base(real) != "system-design.yaml" {
		return "", errors.New("design outside workspace or not system-design.yaml")
	}
	return real, nil
}
func (s *Server) swe(ctx context.Context, mode, file string) ([]byte, error) {
	if s.config.SWEVersion == "" {
		return nil, errors.New("swe_version must pin the selected tool")
	}
	data, code, err := s.command(ctx, s.config.SWE, []string{"version"}, s.config.Root, nil)
	var version struct {
		Version string `json:"version"`
	}
	if err != nil || code != 0 || json.Unmarshal(data, &version) != nil || version.Version != s.config.SWEVersion {
		return nil, errors.New("swe version unavailable or mismatched")
	}
	args := []string{"system-design", mode, "--root", s.config.Root}
	if file != "" {
		args = append(args, "--file", file)
	}
	data, code, err = s.command(ctx, s.config.SWE, args, s.config.Root, nil)
	if err != nil || code != 0 {
		return nil, fmt.Errorf("swe %s failed (exit %d): %s %v", mode, code, strings.TrimSpace(string(data)), err)
	}
	if mode == "render" && !strings.HasPrefix(string(data), "<!doctype html>") {
		return nil, errors.New("swe render did not return HTML")
	}
	return data, nil
}
