package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/AwayerCode/ReedHarness/apps/swe-cli/internal/systemdesign"
)

func TestPreviewArgumentsRejectBypasses(t *testing.T) {
	for _, args := range [][]string{
		{"--file", "../systemdesign/testdata/project/system-design.yaml"},
		{"--file", "../systemdesign/testdata/project/system-design.yaml", "--expect-version", "wrong"},
		{"--file", "preview.html", "--expect-version", Version},
		{"--file", "../systemdesign/testdata/project/system-design.yaml", "--expect-version", Version, "--html", "x"},
		{"--file", "../systemdesign/testdata/project/system-design.yaml", "--expect-version", Version, "--host", "0.0.0.0"},
	} {
		var out bytes.Buffer
		if code := Run(append([]string{"system-design", "preview"}, args...), nil, &out); code != 2 {
			t.Fatalf("%v: %d %s", args, code, out.String())
		}
	}
}

func TestPreviewServerServesAndStops(t *testing.T) {
	p, r, code := systemdesign.NewPreview("../systemdesign/testdata/project/system-design.yaml", Version)
	if code != 0 {
		t.Fatalf("%+v", r)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	done := make(chan int, 1)
	go func() { done <- serveDesignPreview(ctx, listener, p, writer); writer.Close() }()
	var info struct {
		URL     string `json:"url"`
		Version string `json:"generator_version"`
	}
	if err = json.NewDecoder(reader).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Version != Version {
		t.Fatal("unexpected build version")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(info.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !bytes.HasPrefix(body, []byte("<!doctype html>")) {
		t.Fatal("preview not served")
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("shutdown: %d", code)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("server did not stop")
	}
	if response, err := client.Get(info.URL); err == nil {
		response.Body.Close()
		t.Fatal("server still running")
	}
}

func TestPreviewServerWriteTimeoutCoversRendering(t *testing.T) {
	server := newPreviewServer(http.NotFoundHandler())
	if server.WriteTimeout != previewWriteTimeout {
		t.Fatalf("WriteTimeout=%s", server.WriteTimeout)
	}
	if server.WriteTimeout <= 30*time.Second {
		t.Fatalf("WriteTimeout still matches the old render limit: %s", server.WriteTimeout)
	}
}

func TestPreviewCommandProcess(t *testing.T) {
	if raw := os.Getenv("REED_PREVIEW_TEST_ARGS"); raw != "" {
		var args []string
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			os.Exit(2)
		}
		os.Exit(Run(args, nil, os.Stdout))
	}
}

func TestPreviewReuseDoesNotRenderCurrentYAML(t *testing.T) {
	data, err := os.ReadFile("../systemdesign/testdata/project/system-design.yaml")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "system-design.yaml")
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"system-design", "preview", "--file", file, "--expect-version", Version, "--no-open"}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	first := exec.CommandContext(ctx, binary, "-test.run=^TestPreviewCommandProcess$")
	first.Env = append(os.Environ(), "REED_PREVIEW_TEST_ARGS="+string(raw))
	pipe, err := first.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Process.Kill(); _ = first.Wait() }()
	var started struct {
		URL string `json:"url"`
	}
	if err = json.NewDecoder(pipe).Decode(&started); err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(file, []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	repeatCtx, repeatCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer repeatCancel()
	repeat := exec.CommandContext(repeatCtx, binary, "-test.run=^TestPreviewCommandProcess$")
	repeat.Env = first.Env
	out, err := repeat.Output()
	if err != nil {
		t.Fatalf("reuse rendered invalid YAML first: %v\n%s", err, out)
	}
	var reused struct {
		URL    string `json:"url"`
		Reused bool   `json:"reused"`
	}
	if err = json.Unmarshal(out, &reused); err != nil {
		t.Fatal(err)
	}
	if !reused.Reused || reused.URL != started.URL {
		t.Fatalf("running preview was not reused: %s", out)
	}
}

func testPreviewCommand(t *testing.T, args []string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args = append(args, "--no-open")
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestPreviewCommandProcess$")
	cmd.Env = append(os.Environ(), "REED_PREVIEW_TEST_ARGS="+string(raw))
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var info struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}
	if err = json.NewDecoder(pipe).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Format != "swe-preview-v1" {
		t.Fatalf("unexpected preview metadata: %+v", info)
	}

	repeat := exec.CommandContext(ctx, binary, "-test.run=^TestPreviewCommandProcess$")
	repeat.Env = cmd.Env
	reused, err := repeat.Output()
	if err != nil {
		t.Fatal(err)
	}
	var next struct {
		URL           string `json:"url"`
		Reused        bool   `json:"reused"`
		BrowserOpened bool   `json:"browser_opened"`
	}
	if err := json.Unmarshal(reused, &next); err != nil {
		t.Fatal(err)
	}
	if next.URL != info.URL || !next.Reused || next.BrowserOpened {
		t.Fatalf("reuse failed: %s", reused)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(info.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !bytes.HasPrefix(body, []byte("<!doctype html>")) {
		t.Fatal("CLI did not serve rendered HTML")
	}
}
