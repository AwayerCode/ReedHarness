package systemdesign

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPreviewOnlyServesFreshYAMLRendering(t *testing.T) {
	_, data := fixture(t)
	root := t.TempDir()
	file := filepath.Join(root, Filename)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, r, code := NewPreview(file, "pinned-test")
	if code != 0 {
		t.Fatalf("%d %+v", code, r)
	}
	h := p.Handler("127.0.0.1:12345")
	get := func(method, path, host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:12345"+path, nil)
		req.Host = host
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out
	}
	first := get("GET", p.Path(), "127.0.0.1:12345")
	scope, err := FileScope(file)
	if err != nil {
		t.Fatal(err)
	}
	_, expected, code := Execute(scope, "render", "", "pinned-test")
	expected = liveDocument(expected)
	if code != 0 || first.Code != 200 || !bytes.Equal(first.Body.Bytes(), expected) {
		t.Fatal("preview differs from CLI rendering")
	}
	if first.Header().Get("Cache-Control") != "no-store" || !strings.Contains(first.Header().Get("Content-Security-Policy"), "script-src 'sha256-") {
		t.Fatal("missing response protections")
	}
	// A neighbouring HTML file cannot replace or influence the preview.
	if err = os.WriteFile(filepath.Join(root, "preview.html"), []byte("<html>forged</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if out := get("GET", p.Path(), "127.0.0.1:12345"); !bytes.Equal(out.Body.Bytes(), expected) {
		t.Fatal("HTML file influenced preview")
	}
	for _, tc := range []struct {
		method, path, host string
		status             int
	}{
		{"GET", "/preview.html", "127.0.0.1:12345", 404},
		{"GET", p.Path() + "?file=preview.html", "127.0.0.1:12345", 404},
		{"POST", p.Path(), "127.0.0.1:12345", 405},
		{"GET", p.Path(), "attacker.example", 403},
	} {
		if out := get(tc.method, tc.path, tc.host); out.Code != tc.status {
			t.Fatalf("%+v: %d", tc, out.Code)
		}
	}
	if out := get("HEAD", p.Path(), "127.0.0.1:12345"); out.Code != 200 || out.Body.Len() != 0 {
		t.Fatal("HEAD emitted body")
	}
	changed := bytes.Replace(data, []byte("订单处理"), []byte("更新后的订单职责"), 1)
	if err = os.WriteFile(file, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if out := get("GET", p.Path(), "127.0.0.1:12345"); out.Code != 200 || !strings.Contains(out.Body.String(), "更新后的订单职责") {
		t.Fatal("refresh did not reread YAML")
	}
	if err = os.WriteFile(file, []byte("schema_version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out := get("GET", p.Path(), "127.0.0.1:12345"); out.Code != 422 || strings.Contains(out.Body.String(), "<!doctype html>") {
		t.Fatal("invalid YAML served stale page")
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if out := get("GET", p.Path(), "127.0.0.1:12345"); out.Code != 503 {
		t.Fatal("missing YAML served stale page")
	}
}

func TestPreviewRejectsNonYAMLInputs(t *testing.T) {
	for _, name := range []string{"preview.html", Filename} {
		file := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(file, []byte("<html>handwritten</html>"), 0600); err != nil {
			t.Fatal(err)
		}
		if p, _, code := NewPreview(file, "test"); code == 0 || p != nil {
			t.Fatal("accepted HTML input")
		}
	}
}

func TestPreviewSharesConcurrentSnapshotRender(t *testing.T) {
	design := denseOrderingDesign(8, 50, 5)
	data, err := json.Marshal(design)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), Filename)
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	preview := &Preview{file: file, version: "test"}

	const clients = 8
	start := make(chan struct{})
	results := make([]previewRender, clients)
	var wait sync.WaitGroup
	for index := range clients {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index] = preview.current(context.Background())
		}()
	}
	close(start)
	wait.Wait()

	if results[0].code != 0 || len(results[0].html) == 0 {
		t.Fatalf("render failed: %+v", results[0].report)
	}
	first := &results[0].html[0]
	for index := 1; index < clients; index++ {
		if results[index].code != 0 || &results[index].html[0] != first {
			t.Fatal("concurrent requests repeated the snapshot render")
		}
	}
}

func TestPreviewCSPMatchesRenderedScripts(t *testing.T) {
	_, data := fixture(t)
	r := NewReport("render", "test", data)
	d := Parse(data, &r)
	var out bytes.Buffer
	if err := Render(&out, d, r); err != nil {
		t.Fatal(err)
	}
	// html/template may rewrite JavaScript; hashes must use the emitted bytes.
	scripts := strings.Split(out.String(), "<script type=\"application/javascript\">")[1:]
	policy := previewCSP(out.Bytes())
	if len(scripts) == 0 {
		t.Fatal("no scripts")
	}
	for _, s := range scripts {
		sum := sha256.Sum256([]byte(strings.SplitN(s, "</script>", 2)[0]))
		if !strings.Contains(policy, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
			t.Fatal("CSP would block emitted script")
		}
	}
}

func TestLivePreviewStateRecovery(t *testing.T) {
	_, data := fixture(t)
	file := filepath.Join(t.TempDir(), Filename)
	os.WriteFile(file, data, 0600)
	p, _, code := NewPreview(file, "test")
	if code != 0 {
		t.Fatal(code)
	}
	h := p.Handler("127.0.0.1:12345")
	state := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:12345"+p.Path()+"state", nil))
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := state()
	if first["valid"] != true {
		t.Fatal(first)
	}
	os.WriteFile(file, []byte("invalid: ["), 0600)
	if state()["valid"] != false {
		t.Fatal("invalid YAML not detected")
	}
	os.Remove(file)
	if state()["valid"] != false {
		t.Fatal("deleted YAML not detected")
	}
	os.WriteFile(file, data, 0600)
	recovered := state()
	if recovered["valid"] != true || recovered["revision"] != first["revision"] {
		t.Fatal("recovery failed")
	}
	os.WriteFile(file, bytes.Replace(data, []byte("订单处理"), []byte("新的职责"), 1), 0600)
	if state()["revision"] == first["revision"] {
		t.Fatal("change not detected")
	}
}

// Exercise the shipped browser monitor, including error and reconnect paths.
func TestLivePreviewBrowserMonitor(t *testing.T) {
	script := strings.TrimSuffix(strings.TrimPrefix(liveScript, "<script type=\"application/javascript\">"), "</script>")
	harness := `
const vm = require("node:vm"), assert = require("node:assert/strict");
let callback, overlay, reloads = 0, timeout, state = {valid:true, revision:"REVISION"}, disconnected = false;
const context = {
  URL,
  AbortSignal:{timeout(ms){timeout=ms;return "abort-signal";}},
  location:{href:"http://127.0.0.1:1234/token/#info-order", reload(){reloads++;}},
  document:{createElement(){return {style:{}, setAttribute(){}, remove(){overlay=null;}};},body:{appendChild(el){overlay=el;}}},
  setTimeout(fn){callback=fn;},
  async fetch(url, options){assert.equal(url.href,"http://127.0.0.1:1234/token/state");assert.equal(options.signal,"abort-signal");if(disconnected)throw Error("offline");return {ok:true,json:async()=>state};}
};
vm.runInNewContext(process.argv[1],context);
(async()=>{
 await callback(); assert.equal(timeout,60000); assert.equal(reloads,0); assert.equal(overlay,undefined);
 state={valid:false,report:{message:"bad YAML"}}; await callback(); assert.match(overlay.textContent,/已过期/);
 state={valid:true,revision:"REVISION"}; await callback(); assert.equal(overlay,null);
 disconnected=true; await callback(); assert.match(overlay.textContent,/连接中断/);
 disconnected=false; await callback(); assert.equal(overlay,null);
 state={valid:true,revision:"changed"}; await callback(); assert.equal(reloads,1);
})().catch(error=>{console.error(error);process.exitCode=1;});
`
	cmd := exec.Command("node", "-e", harness, script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser monitor: %v\n%s", err, output)
	}
}
