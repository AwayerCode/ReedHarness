package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type archiveFile struct {
	body string
	mode int64
}

type archiveFiles map[string]archiveFile

type archiveServer struct {
	mu      sync.RWMutex
	payload []byte
	URL     string
}

func newArchiveServer(t *testing.T, files archiveFiles) *archiveServer {
	t.Helper()
	return &archiveServer{
		payload: makeArchive(t, files),
		URL:     "https://example.test/archive.tar.gz",
	}
}

func (s *archiveServer) Client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		s.mu.RLock()
		current := append([]byte(nil), s.payload...)
		s.mu.RUnlock()
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Body:          io.NopCloser(bytes.NewReader(current)),
			ContentLength: int64(len(current)),
			Header:        make(http.Header),
			Request:       request,
		}, nil
	})}
}

func (s *archiveServer) Set(files archiveFiles) {
	s.mu.Lock()
	s.payload = makeArchiveWithoutTest(files)
	s.mu.Unlock()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func makeArchive(t *testing.T, files archiveFiles) []byte {
	t.Helper()
	return makeArchiveWithoutTest(files)
}

func makeArchiveWithoutTest(files archiveFiles) []byte {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		file := files[name]
		header := &tar.Header{
			Name:     "ReedHarness-main/" + name,
			Mode:     file.mode,
			Size:     int64(len(file.body)),
			Typeflag: tar.TypeReg,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			panic(err)
		}
		if _, err := tarWriter.Write([]byte(file.body)); err != nil {
			panic(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		panic(err)
	}
	if err := gzipWriter.Close(); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

func sortStrings(values []string) {
	for left := 0; left < len(values); left++ {
		for right := left + 1; right < len(values); right++ {
			if values[right] < values[left] {
				values[left], values[right] = values[right], values[left]
			}
		}
	}
}

func assertFile(t *testing.T, project, name, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(project, ".agents", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", name, data, want)
	}
}

func writeProjectFile(t *testing.T, project, name, body string, mode os.FileMode) {
	t.Helper()
	target := filepath.Join(project, ".agents", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func skillBody(name string) string {
	return "---\nname: " + name + "\ndescription: test skill\n---\n# Test\n"
}

func TestInstallOnlySelectedSkillWithoutManagementFiles(t *testing.T) {
	for _, prefix := range []string{"skills", "Skills"} {
		t.Run(prefix, func(t *testing.T) {
			archive := newArchiveServer(t, archiveFiles{
				prefix + "/swe/SKILL.md":         {body: skillBody("swe"), mode: 0644},
				prefix + "/swe/scripts/check.sh": {body: "#!/bin/sh\n", mode: 0755},
				prefix + "/other/SKILL.md":       {body: skillBody("other"), mode: 0644},
			})
			project := t.TempDir()
			manager := Manager{ProjectRoot: project, Client: archive.Client()}
			results, err := manager.Install(context.Background(), archive.URL, "tag:v1", []string{prefix + "/swe"})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Path != ".agents/skills/swe" || results[0].Files != 2 {
				t.Fatalf("results: %+v", results)
			}
			assertFile(t, project, "skills/swe/SKILL.md", skillBody("swe"))
			info, err := os.Stat(filepath.Join(project, ".agents", "skills", "swe", "scripts", "check.sh"))
			if err != nil || info.Mode().Perm()&0111 == 0 {
				t.Fatalf("executable: %v %v", info, err)
			}
			var files []string
			err = filepath.WalkDir(project, func(name string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					relative, _ := filepath.Rel(project, name)
					files = append(files, filepath.ToSlash(relative))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(files, "|") != ".agents/skills/swe/SKILL.md|.agents/skills/swe/scripts/check.sh" {
				t.Fatalf("unexpected project files: %v", files)
			}
			skills, err := manager.List()
			if err != nil || len(skills) != 1 || skills[0].Name != "swe" {
				t.Fatalf("list: %+v %v", skills, err)
			}
		})
	}
}

func TestInstallDryRunAndInvalidPackagesLeaveProjectUntouched(t *testing.T) {
	for _, test := range []struct {
		name, body string
		dry        bool
		fail       bool
	}{
		{"dry-run", skillBody("swe"), true, false}, {"invalid", "not a Skill", false, true}, {"mismatch", skillBody("other"), false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := t.TempDir()
			archive := newArchiveServer(t, archiveFiles{"skills/swe/SKILL.md": {body: test.body, mode: 0644}})
			manager := Manager{ProjectRoot: project, Client: archive.Client(), DryRun: test.dry}
			_, err := manager.Install(context.Background(), archive.URL, "tag:v1", []string{"skills/swe"})
			if (err != nil) != test.fail {
				t.Fatalf("error: %v", err)
			}
			entries, err := os.ReadDir(project)
			if err != nil || len(entries) != 0 {
				t.Fatalf("project changed: %v %v", entries, err)
			}
		})
	}
}

func TestExistingSkillIsNeverOverwritten(t *testing.T) {
	project := t.TempDir()
	writeProjectFile(t, project, "skills/swe/SKILL.md", "local", 0644)
	archive := newArchiveServer(t, archiveFiles{"skills/swe/SKILL.md": {body: skillBody("swe"), mode: 0644}, "skills/other/SKILL.md": {body: skillBody("other"), mode: 0644}})
	manager := Manager{ProjectRoot: project, Client: archive.Client()}
	if _, err := manager.Install(context.Background(), archive.URL, "tag:v1", []string{"skills/other", "skills/swe"}); err == nil {
		t.Fatal("expected conflict")
	}
	assertFile(t, project, "skills/swe/SKILL.md", "local")
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "other")); !os.IsNotExist(err) {
		t.Fatalf("partial batch installed: %v", err)
	}
}

func TestListFindsManuallyInstalledSkillsWithoutWriting(t *testing.T) {
	project := t.TempDir()
	writeProjectFile(t, project, "skills/custom/SKILL.md", skillBody("custom"), 0644)
	writeProjectFile(t, project, "skills/incomplete/readme.md", "incomplete", 0644)
	entries, err := (Manager{ProjectRoot: project}).List()
	if err != nil || len(entries) != 1 || entries[0].Name != "custom" {
		t.Fatalf("list: %v %v", entries, err)
	}
	empty := t.TempDir()
	entries, err = (Manager{ProjectRoot: empty}).List()
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty list: %v %v", entries, err)
	}
	files, _ := os.ReadDir(empty)
	if len(files) != 0 {
		t.Fatal("list wrote files")
	}
}

func TestRejectsUnsafeAndNonSkillPaths(t *testing.T) {
	for _, target := range []string{"README.md", "skills", "skills/swe/SKILL.md", "../swe", "skills/../swe", "skills/CON", "skills/a:b"} {
		if _, err := normalizeTargets([]string{target}); err == nil {
			t.Errorf("accepted %q", target)
		}
	}
	if _, err := normalizeTargets([]string{"skills/swe", "Skills/swe"}); err == nil {
		t.Fatal("destination collision accepted")
	}
	targets, err := normalizeTargets([]string{`skills\swe`, "skills/swe/"})
	if err != nil || len(targets) != 1 {
		t.Fatalf("normalize: %v %v", targets, err)
	}
}

func TestRejectsSymlinkParent(t *testing.T) {
	project, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, ".agents")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	manager := Manager{ProjectRoot: project, Client: http.DefaultClient}
	if _, err := manager.Install(context.Background(), "https://example.test/archive.tar.gz", "tag:v1", []string{"skills/swe"}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("got %v", err)
	}
	files, _ := os.ReadDir(outside)
	if len(files) != 0 {
		t.Fatal("wrote outside project")
	}
}

func TestCancelledPublishCleansPartialSkill(t *testing.T) {
	project := t.TempDir()
	archive := newArchiveServer(t, archiveFiles{"skills/swe/SKILL.md": {body: skillBody("swe"), mode: 0644}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manager := Manager{ProjectRoot: project, Client: archive.Client()}
	if _, err := manager.Install(ctx, archive.URL, "tag:v1", []string{"skills/swe"}); err == nil {
		t.Fatal("cancelled install succeeded")
	}
	files, _ := os.ReadDir(project)
	if len(files) != 0 {
		t.Fatalf("partial install: %v", files)
	}
}

func TestConcurrentInstallDoesNotOverwriteWinner(t *testing.T) {
	project := t.TempDir()
	archive := newArchiveServer(t, archiveFiles{"skills/swe/SKILL.md": {body: skillBody("swe"), mode: 0644}})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager := Manager{ProjectRoot: project, Client: archive.Client()}
			_, err := manager.Install(context.Background(), archive.URL, "tag:v1", []string{"skills/swe"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful installs: %d", success)
	}
	assertFile(t, project, "skills/swe/SKILL.md", skillBody("swe"))
}

func TestLateConflictRollsBackOnlyThisInstall(t *testing.T) {
	project := t.TempDir()
	archive := newArchiveServer(t, archiveFiles{
		"skills/alpha/SKILL.md": {body: skillBody("alpha"), mode: 0644},
		"skills/zeta/SKILL.md":  {body: skillBody("zeta"), mode: 0644},
	})
	client := archive.Client()
	transport := client.Transport
	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		writeProjectFile(t, project, "skills/zeta/SKILL.md", "concurrent owner", 0644)
		return transport.RoundTrip(request)
	})
	manager := Manager{ProjectRoot: project, Client: client}
	if _, err := manager.Install(context.Background(), archive.URL, "tag:v1", []string{"skills/alpha", "skills/zeta"}); err == nil {
		t.Fatal("expected late conflict")
	}
	assertFile(t, project, "skills/zeta/SKILL.md", "concurrent owner")
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("partial batch retained: %v", err)
	}
}

func TestThirdPartyInstallCompatibility(t *testing.T) {
	for _, requested := range []string{"skills/external", "skills/3rdparty/external"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%v", requested, dryRun), func(t *testing.T) {
				project := t.TempDir()
				archive := newArchiveServer(t, archiveFiles{
					"skills/index.json":                       {body: `{"schema_version":1,"skills":[{"name":"external","path":"skills/3rdparty/external","description":"test"}]}`, mode: 0644},
					"skills/3rdparty/external/SKILL.md":       {body: skillBody("external"), mode: 0644},
					"skills/3rdparty/external/LICENSE":        {body: "license preserved", mode: 0644},
					"skills/3rdparty/external/scripts/run.sh": {body: "#!/bin/sh\n", mode: 0755},
					"skills/3rdparty/unselected/SKILL.md":     {body: skillBody("unselected"), mode: 0644},
				})
				manager := Manager{ProjectRoot: project, Client: archive.Client(), DryRun: dryRun}
				results, err := manager.Install(context.Background(), archive.URL, "commit:fixed", []string{requested})
				if err != nil {
					t.Fatal(err)
				}
				if len(results) != 1 || results[0].Path != ".agents/skills/external" || results[0].Files != 3 {
					t.Fatal(results)
				}
				if dryRun {
					entries, _ := os.ReadDir(project)
					if len(entries) != 0 {
						t.Fatal("dry run wrote project")
					}
					return
				}
				base := filepath.Join(project, ".agents", "skills", "external")
				data, _ := os.ReadFile(filepath.Join(base, "LICENSE"))
				if string(data) != "license preserved" {
					t.Fatal("license lost")
				}
				info, err := os.Stat(filepath.Join(base, "scripts", "run.sh"))
				if err != nil || info.Mode().Perm()&0111 == 0 {
					t.Fatal("executable lost")
				}
				entries, _ := os.ReadDir(filepath.Dir(base))
				if len(entries) != 1 {
					t.Fatal("extra Skills installed")
				}
				if _, err := manager.Install(context.Background(), archive.URL, "commit:fixed", []string{requested}); err == nil {
					t.Fatal("overwrote installed Skill")
				}
			})
		}
	}
}
func TestThirdPartyRejectsAmbiguousAndUnsafeTargets(t *testing.T) {
	for _, targets := range [][]string{
		{"skills/external", "skills/3rdparty/external"},
		{"skills/3rdparty"}, {"skills/3rdparty/../external"},
		{"skills/3rdparty/external/references"},
		{"skills/other/external"},
	} {
		if _, err := normalizeTargets(targets); err == nil {
			t.Fatalf("accepted %v", targets)
		}
	}
}
func TestCatalogCannotRedirectInstallOutsideSkill(t *testing.T) {
	for _, body := range []string{
		`{"schema_version":1,"skills":[{"name":"external","path":"../external","description":"test"}]}`,
		`{"schema_version":1,"skills":[{"name":"external","path":"skills/3rdparty/other","description":"test"}]}`,
		`broken`,
	} {
		project := t.TempDir()
		archive := newArchiveServer(t, archiveFiles{"skills/index.json": {body: body, mode: 0644}, "skills/external/SKILL.md": {body: skillBody("external"), mode: 0644}})
		manager := Manager{ProjectRoot: project, Client: archive.Client()}
		if _, err := manager.Install(context.Background(), archive.URL, "tag:v1", []string{"skills/external"}); err == nil {
			t.Fatal("accepted invalid catalog")
		}
		entries, _ := os.ReadDir(project)
		if len(entries) != 0 {
			t.Fatal("invalid install wrote project")
		}
	}
}
