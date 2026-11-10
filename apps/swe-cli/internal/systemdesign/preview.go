package systemdesign

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Preview owns only a YAML path and the running build's renderer. It never
// accepts HTML, serves a directory, loads a template, or invokes another binary.
type Preview struct {
	file, version, route string

	mu        sync.Mutex
	cachedKey string
	cached    previewRender
	loading   map[string]*previewLoad
}

type previewRender struct {
	report Report
	html   []byte
	code   int
}

type previewLoad struct {
	done chan struct{}
}

type previewSnapshot struct {
	scope Scope
	data  []byte
	key   string
	err   error
}

func NewPreview(file, version string) (*Preview, Report, int) {
	scope, err := FileScope(file)
	if err != nil {
		return nil, UnavailableReport("preview", version, err), 2
	}
	preview := &Preview{file: scope.File, version: version}
	rendered := preview.current(context.Background())
	if rendered.code != 0 {
		return nil, rendered.report, rendered.code
	}
	var token [24]byte
	if _, err = rand.Read(token[:]); err != nil {
		return nil, UnavailableReport("preview", version, err), 2
	}
	preview.route = "/" + hex.EncodeToString(token[:]) + "/"
	return preview, rendered.report, 0
}

func (p *Preview) Path() string { return p.route }
func (p *Preview) File() string { return p.file }

// Host is the literal loopback listener address, preventing Host-based rebinding.
// Every valid request gets a fresh scope and one read of the YAML input. Errors
// replace the document; no prior successful page is retained or served on failure.
func (p *Preview) Handler(host string) http.Handler {
	return p.handler(host, "'none'")
}

func (p *Preview) handler(host, ancestors string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors "+ancestors+"; base-uri 'none'")
		if r.Host != host {
			http.Error(w, "invalid preview host", http.StatusForbidden)
			return
		}
		if (r.URL.Path != p.route && r.URL.Path != p.route+"state") || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "read-only preview", http.StatusMethodNotAllowed)
			return
		}
		rendered := p.current(r.Context())
		report, html, code := rendered.report, rendered.html, rendered.code
		if r.URL.Path == p.route+"state" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sum := sha256.Sum256(html)
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{"valid": code == 0, "revision": hex.EncodeToString(sum[:]), "report": report})
			}
			return
		}
		if code != 0 {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			status := http.StatusUnprocessableEntity
			if code == 2 {
				status = http.StatusServiceUnavailable
			}
			w.WriteHeader(status)
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(report)
			}
			return
		}
		html = liveDocument(html)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", strings.Replace(previewCSP(html), "frame-ancestors 'none'", "frame-ancestors "+ancestors, 1))
		if r.Method == http.MethodGet {
			_, _ = w.Write(html)
		}
	})
}

func previewSnapshotKey(scope Scope, data []byte) string {
	meta, _ := json.Marshal(struct {
		Unavailable string
		File        string
		Root        string
		Children    []string
	}{scope.Unavailable, scope.File, scope.Root, scope.Children})
	dataDigest := sha256.Sum256(data)
	digest := sha256.New()
	_, _ = digest.Write(meta)
	_, _ = digest.Write(dataDigest[:])
	return hex.EncodeToString(digest.Sum(nil))
}

func previewErrorKey(file string, err error) string {
	digest := sha256.Sum256([]byte(file + "\x00" + err.Error()))
	return hex.EncodeToString(digest[:])
}

func (p *Preview) snapshot() previewSnapshot {
	scope, err := FileScope(p.file)
	if err != nil {
		return previewSnapshot{
			scope: Scope{File: p.file},
			key:   previewErrorKey(p.file, err),
			err:   err,
		}
	}
	data, err := readScopeInput(scope)
	if err != nil {
		return previewSnapshot{
			scope: scope,
			key:   previewSnapshotKey(scope, []byte(err.Error())),
			err:   err,
		}
	}
	return previewSnapshot{scope: scope, data: data, key: previewSnapshotKey(scope, data)}
}

// current renders each stable filesystem snapshot once. Concurrent page and
// state requests for the same YAML wait for that result instead of repeating
// the expensive layout work.
func (p *Preview) current(ctx context.Context) previewRender {
	for {
		snapshot := p.snapshot()

		p.mu.Lock()
		if p.cachedKey == snapshot.key {
			rendered := p.cached
			p.mu.Unlock()
			return rendered
		}
		if load := p.loading[snapshot.key]; load != nil {
			p.mu.Unlock()
			select {
			case <-load.done:
				continue
			case <-ctx.Done():
				return previewRender{
					report: UnavailableReport("preview", p.version, ctx.Err()),
					code:   2,
				}
			}
		}
		done := make(chan struct{})
		if p.loading == nil {
			p.loading = map[string]*previewLoad{}
		}
		p.loading[snapshot.key] = &previewLoad{done: done}
		p.mu.Unlock()

		var rendered previewRender
		if snapshot.err != nil {
			rendered = previewRender{
				report: UnavailableReport("preview", p.version, snapshot.err),
				code:   2,
			}
		} else {
			rendered.report, rendered.html, rendered.code = NewSession().executeData(
				snapshot.scope,
				snapshot.data,
				"render",
				"",
				p.version,
			)
		}

		p.mu.Lock()
		p.cachedKey = snapshot.key
		p.cached = rendered
		delete(p.loading, snapshot.key)
		close(done)
		p.mu.Unlock()
		return rendered
	}
}

// Permit only the scripts embedded in this build. Dynamic inline styles support
// pan/zoom and column resizing; no external resources or network calls are used.
func previewCSP(html []byte) string {
	hashes := []string{}
	for _, match := range regexp.MustCompile(`(?s)<script type="application/javascript">(.*?)</script>`).FindAllStringSubmatch(string(html), -1) {
		sum := sha256.Sum256([]byte(match[1]))
		hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return "default-src 'none'; script-src " + strings.Join(hashes, " ") + "; connect-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'"
}

// The live wrapper is fixed CLI code; the offline renderer remains deterministic.
func liveDocument(html []byte) []byte {
	sum := sha256.Sum256(html)
	script := strings.ReplaceAll(liveScript, "REVISION", hex.EncodeToString(sum[:]))
	return []byte(strings.Replace(string(html), "</body>", script+"</body>", 1))
}

const liveScript = `<script type="application/javascript">
(function () {
  const revision = "REVISION";
  let warning;
  function show(message) {
    if (!warning) {
      warning = document.createElement("div");
      warning.setAttribute("role", "alert");
      warning.style.cssText = "position:fixed;inset:0;z-index:2147483647;background:rgba(20,25,35,.94);color:#fff;padding:8vh 8vw;overflow:auto;white-space:pre-wrap;font:14px/1.6 system-ui";
      document.body.appendChild(warning);
    }
    warning.textContent = message;
  }
  async function check() {
    try {
      const stateURL = new URL("state", location.href);
      stateURL.search = new URL(location.href).search;
      const response = await fetch(stateURL, {cache:"no-store", signal:AbortSignal.timeout(60000)});
      if (!response.ok) throw new Error("HTTP " + response.status);
      const state = await response.json();
      if (!state.valid) {
        show("当前架构图已过期 · YAML 校验失败，修复后自动更新\n\n" + JSON.stringify(state.report, null, 2));
      } else if (state.revision !== revision) {
        location.reload();
        return;
      } else if (warning) {
        warning.remove();
        warning = null;
      }
    } catch (error) {
      show("当前架构图可能已过期 · 预览服务连接中断，正在重试\n\n" + error.message);
    }
    setTimeout(check, 1000);
  }
  setTimeout(check, 1000);
})();
</script>`
