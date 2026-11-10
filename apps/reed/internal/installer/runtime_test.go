package installer

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallBundledCLIWithoutAdditionalRequests(t *testing.T) {
	for _, name := range []string{"swe-cli", "swe-cli.exe"} {
		t.Run(name, func(t *testing.T) {
			archive := newArchiveServer(t, archiveFiles{
				"skills/swe/SKILL.md":     {body: skillBody("swe"), mode: 0644},
				"skills/swe/bin/" + name:  {body: "bundled executable", mode: 0755},
				"skills/swe/runtime.json": {body: `{"name":"swe-cli","assets":{"local":{"sha256":"package-owned metadata"}}}`, mode: 0644},
			})
			client := archive.Client()
			transport := client.Transport
			requests := 0
			client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.URL.String() != archive.URL {
					t.Fatalf("unexpected external request: %s", request.URL)
				}
				return transport.RoundTrip(request)
			})
			project := t.TempDir()
			manager := Manager{ProjectRoot: project, Client: client}
			if _, err := manager.Install(context.Background(), archive.URL, "tag:v1", []string{"skills/swe"}); err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("requests: %d", requests)
			}
			assertFile(t, project, "skills/swe/bin/"+name, "bundled executable")
			assertFile(t, project, "skills/swe/runtime.json", `{"name":"swe-cli","assets":{"local":{"sha256":"package-owned metadata"}}}`)
			info, err := os.Stat(filepath.Join(project, ".agents", "skills", "swe", "bin", name))
			if err != nil || info.Mode().Perm()&0111 == 0 {
				t.Fatalf("executable permission: %v %v", info, err)
			}
		})
	}
}

func TestInstallDoesNotInterpretLegacyDownloadManifest(t *testing.T) {
	archive := newArchiveServer(t, archiveFiles{
		"skills/swe/SKILL.md":     {body: skillBody("swe"), mode: 0644},
		"skills/swe/runtime.json": {body: `{"url":"https://example.invalid/must-not-fetch"}`, mode: 0644},
	})
	client := archive.Client()
	transport := client.Transport
	requests := 0
	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) { requests++; return transport.RoundTrip(request) })
	project := t.TempDir()
	manager := Manager{ProjectRoot: project, Client: client}
	if _, err := manager.Install(context.Background(), archive.URL, "tag:v1", []string{"skills/swe"}); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("runtime metadata triggered extra download: %d", requests)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "swe", "bin")); !os.IsNotExist(err) {
		t.Fatalf("installer manufactured CLI: %v", err)
	}
}
