package systemdesign

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceProjectsRefreshAndBoundaries(t *testing.T) {
	_, data := fixture(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(project, Filename)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root, "test", "127.0.0.1:47831")
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "http://127.0.0.1:47831"+path, nil)
		response := httptest.NewRecorder()
		service.ServeHTTP(response, request)
		return response
	}
	if response := get("/reed/"); response.Code != 200 || !strings.Contains(response.Body.String(), "view?project=project") {
		t.Fatal(response)
	}
	response := get("/reed/view?project=project")
	resolved := get("/reed/resolve?cwd=" + url.QueryEscape(project))
	if resolved.Code != 200 || !strings.Contains(resolved.Body.String(), "project=project") {
		t.Fatal(resolved)
	}
	if get("/reed/resolve?cwd="+url.QueryEscape(t.TempDir())).Code != 403 {
		t.Fatal("outside session accepted")
	}
	if get("/reed/resolve?cwd=relative").Code != 400 {
		t.Fatal("relative session accepted")
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(project, alias); err == nil {
		if result := get("/reed/resolve?cwd=" + url.QueryEscape(alias)); result.Code != 200 || result.Body.String() != resolved.Body.String() {
			t.Fatal(result)
		}
	}
	if runtime.GOOS == "windows" {
		if result := get("/reed/resolve?cwd=" + url.QueryEscape(strings.ToUpper(project))); result.Code != 200 {
			t.Fatal(result)
		}
	}
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") || !strings.Contains(response.Body.String(), "stateURL.search") {
		t.Fatal(response)
	}
	state := func() map[string]any {
		result := map[string]any{}
		if err := json.Unmarshal(get("/reed/state?project=project").Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := state()
	if before["valid"] != true {
		t.Fatal(before)
	}
	if err := os.WriteFile(file, []byte("broken: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if state()["valid"] != false {
		t.Fatal("stale valid state")
	}
	if get("/reed/view?project=project").Code == 200 {
		t.Fatal("served stale HTML")
	}
	if !strings.Contains(get("/reed/view?project=project").Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatal("diagnostics cannot be embedded")
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if state()["valid"] != true || state()["revision"] != before["revision"] {
		t.Fatal("recovery failed")
	}
	for _, path := range []string{"/reed/view?project=..", "/reed/view?project=/etc", "/reed/view?project=.git"} {
		if get(path).Code != 403 {
			t.Fatal(path)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err == nil {
		if get("/reed/view?project=escape").Code != 403 {
			t.Fatal("symlink escaped")
		}
	}
	for _, kind := range []string{"host", "method", "origin"} {
		request := httptest.NewRequest("GET", "http://127.0.0.1:47831/reed/", nil)
		switch kind {
		case "host":
			request.Host = "attacker.test"
		case "method":
			request.Method = "POST"
		case "origin":
			request.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		response := httptest.NewRecorder()
		service.ServeHTTP(response, request)
		if response.Code < 400 {
			t.Fatal(kind)
		}
	}
}
