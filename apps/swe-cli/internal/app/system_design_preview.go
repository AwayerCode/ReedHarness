package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/AwayerCode/ReedHarness/apps/swe-cli/internal/systemdesign"
)

func systemDesignPreview(args []string, out io.Writer) int {
	fail := func(err error) int { return encode(out, systemdesign.UnavailableReport("preview", Version, err), 2) }
	f := flag.NewFlagSet("system-design preview", flag.ContinueOnError)
	root := f.String("root", ".", "base for a relative YAML path")
	file := f.String("file", "", "system-design.yaml path")
	expected := f.String("expect-version", "", "required exact CLI version")
	noOpen := f.Bool("no-open", false, "do not open the system browser")
	help := setHelp(f, out, "swe-cli system-design preview --file <system-design.yaml> --expect-version <version>\nServes fresh CLI output on a random loopback port until stopped. Automatically reloads YAML changes and opens a browser; --no-open disables opening.")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return help()
		}
		return fail(err)
	}
	if f.NArg() != 0 || *file == "" {
		return fail(errors.New("preview requires --file and accepts no positional arguments"))
	}
	if *expected == "" || *expected != Version {
		return fail(fmt.Errorf("--expect-version must match this CLI build (%s)", Version))
	}
	path := *file
	if !filepath.IsAbs(path) {
		path = filepath.Join(*root, path)
	}
	scope, err := systemdesign.FileScope(path)
	if err != nil {
		return fail(err)
	}
	key, err := previewKey(scope.File)
	if err != nil {
		return fail(err)
	}
	if info, ok := findExistingPreview(key, scope.File); ok {
		info["reused"] = true
		openPreview(info, *noOpen)
		return encode(out, info, 0)
	}
	preview, report, code := systemdesign.NewPreview(scope.File, Version)
	if code != 0 {
		return encode(out, report, code)
	}
	for attempt := 0; attempt < 32; attempt++ {
		port := 20000 + (int(key[0])*256+int(key[1])+attempt)%40000
		host := fmt.Sprintf("127.0.0.1:%d", port)
		identityPath := "/" + hex.EncodeToString(key[:]) + "/service"
		if info, ok := existingPreview(host, identityPath, preview.File()); ok {
			info["reused"] = true
			openPreview(info, *noOpen)
			return encode(out, info, 0)
		}
		listener, err := net.Listen("tcp4", host)
		if err != nil {
			// Another invocation may have bound the socket but not started serving.
			for retry := 0; retry < 10; retry++ {
				if info, ok := existingPreview(host, identityPath, preview.File()); ok {
					info["reused"] = true
					openPreview(info, *noOpen)
					return encode(out, info, 0)
				}
				time.Sleep(50 * time.Millisecond)
			}
			continue
		}
		defer listener.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return servePreview(ctx, listener, preview, out, identityPath, !*noOpen)
	}
	return fail(errors.New("no available preview port"))
}

func serveDesignPreview(ctx context.Context, listener net.Listener, preview *systemdesign.Preview, out io.Writer) int {
	return servePreview(ctx, listener, preview, out, "", false)
}

func servePreview(ctx context.Context, listener net.Listener, preview *systemdesign.Preview, out io.Writer, identityPath string, autoOpen bool) int {
	host := listener.Addr().String()
	info := map[string]any{"format": "swe-preview-v1", "url": "http://" + host + preview.Path(), "source": preview.File(), "generator_version": Version, "reused": false}
	page := preview.Handler(host)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if identityPath != "" && r.URL.Path == identityPath && r.URL.RawQuery == "" && r.Host == host && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(map[string]any{"format": "swe-preview-v1", "url": "http://" + host + preview.Path(), "source": preview.File(), "generator_version": Version})
			return
		}
		page.ServeHTTP(w, r)
	})
	server := newPreviewServer(handler)
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	openPreview(info, !autoOpen)
	if code := encode(out, info, 0); code != 0 {
		_ = server.Close()
		<-result
		return 2
	}
	select {
	case err := <-result:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return encode(out, systemdesign.UnavailableReport("preview", Version, err), 2)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
		<-result
	}
	return 0
}

const (
	previewWriteTimeout    = 2 * time.Minute
	previewIdentityTimeout = 250 * time.Millisecond
)

func newPreviewServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      previewWriteTimeout,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8192,
	}
}

// Include executable bytes so distinct builds with the same version cannot reuse
// each other's renderer. Canonical FileScope paths identify the YAML.
func previewKey(file string) ([32]byte, error) {
	executable, err := os.Executable()
	if err != nil {
		return [32]byte{}, err
	}
	f, err := os.Open(executable)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append(h.Sum(nil), []byte("\x00"+Version+"\x00"+file)...)), nil
}

func findExistingPreview(key [32]byte, file string) (map[string]any, bool) {
	for attempt := 0; attempt < 32; attempt++ {
		port := 20000 + (int(key[0])*256+int(key[1])+attempt)%40000
		host := fmt.Sprintf("127.0.0.1:%d", port)
		identityPath := "/" + hex.EncodeToString(key[:]) + "/service"
		if info, ok := existingPreview(host, identityPath, file); ok {
			return info, true
		}
	}
	return nil, false
}

func existingPreview(host, path, file string) (map[string]any, bool) {
	client := &http.Client{Timeout: previewIdentityTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://" + host + path)
	if err != nil {
		return nil, false
	}
	defer response.Body.Close()
	var info map[string]any
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&info) != nil {
		return nil, false
	}
	url, _ := info["url"].(string)
	if info["format"] != "swe-preview-v1" || info["source"] != file || info["generator_version"] != Version || !strings.HasPrefix(url, "http://"+host+"/") {
		return nil, false
	}
	return info, true
}

func openPreview(info map[string]any, disabled bool) {
	if disabled {
		info["browser_opened"] = false
		return
	}
	url := info["url"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	err := cmd.Run()
	info["browser_opened"] = err == nil
	if err != nil {
		info["browser_error"] = err.Error()
	}
}
