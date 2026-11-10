package systemdesign

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Service struct {
	root, version, host string
	mu                  sync.Mutex
	previews            map[string]*Preview
}

func NewService(root, version, host string) (*Service, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace is not a directory")
	}
	return &Service{root: root, version: version, host: host, previews: map[string]*Preview{}}, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'; base-uri 'none'")
	if r.Host != s.host {
		http.Error(w, "invalid service host", 403)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "read-only service", 405)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "cross-site access denied", 403)
		return
	}
	switch r.URL.Path {
	case "/":
		http.Redirect(w, r, "/reed/", http.StatusTemporaryRedirect)
	case "/reed/health":
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(map[string]string{"service": "reed", "version": s.version, "root": s.root})
		}
	case "/reed/resolve":
		cwd := r.URL.Query().Get("cwd")
		if !filepath.IsAbs(cwd) {
			http.Error(w, "absolute session directory required", 400)
			return
		}
		root, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			http.Error(w, "session directory unavailable", 403)
			return
		}
		rel, err := filepath.Rel(s.root, root)
		if err != nil || (rel != "." && !within(s.root, root)) || gitMetadata(root) {
			http.Error(w, "session outside workspace", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(map[string]string{"url": "/reed/view?project=" + url.QueryEscape(filepath.ToSlash(rel))})
		}
	case "/reed/":
		discovery, err := Discover(s.root)
		if err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		type entry struct{ Name, URL string }
		entries := []entry{}
		for _, scope := range discovery.Scopes {
			rel, _ := filepath.Rel(s.root, scope.Root)
			entries = append(entries, entry{rel, "view?project=" + url.QueryEscape(filepath.ToSlash(rel))})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == "GET" {
			_ = serviceIndex.Execute(w, struct {
				Entries []entry
				Issues  []DiscoveryIssue
			}{entries, discovery.Issues})
		}
	case "/reed/view", "/reed/state":
		project := r.URL.Query().Get("project")
		if project == "" {
			project = "."
		}
		root, err := filepath.EvalSymlinks(filepath.Join(s.root, filepath.FromSlash(project)))
		if err != nil || (root != s.root && !within(s.root, root)) || filepath.IsAbs(project) || gitMetadata(root) {
			http.Error(w, "project outside workspace or unavailable", 403)
			return
		}
		file := filepath.Join(root, Filename)
		s.mu.Lock()
		preview := s.previews[file]
		if preview == nil {
			preview = &Preview{file: file, version: s.version, route: "/preview/", loading: map[string]*previewLoad{}}
			s.previews[file] = preview
		}
		s.mu.Unlock()
		request := r.Clone(r.Context())
		request.URL.Path = preview.route
		if strings.HasSuffix(r.URL.Path, "/state") {
			request.URL.Path += "state"
		}
		request.URL.RawQuery = ""
		preview.handler(s.host, "'self'").ServeHTTP(w, request)
	default:
		http.NotFound(w, r)
	}
}

var serviceIndex = template.Must(template.New("projects").Parse(`<!doctype html><html lang="zh"><meta charset="utf-8"><title>ReedHarness</title><body><h1>ReedHarness 架构</h1><ul>{{range .Entries}}<li><a href="{{.URL}}">{{.Name}}</a></li>{{else}}<li>工作区尚无 system-design.yaml</li>{{end}}</ul>{{range .Issues}}<p>{{.Path}}: {{.Message}}</p>{{end}}</body></html>`))
