package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveArchiveURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		source  string
		version string
		want    string
		wantErr bool
	}{
		{
			name:    "branch",
			source:  "https://github.com/AwayerCode/ReedHarness.git",
			version: "branch:main",
			want:    "https://github.com/AwayerCode/ReedHarness/archive/refs/heads/main.tar.gz",
		},
		{
			name:    "tag with slash",
			source:  "https://github.com/AwayerCode/ReedHarness",
			version: "tag:release/2026.10",
			want:    "https://github.com/AwayerCode/ReedHarness/archive/refs/tags/release/2026.10.tar.gz",
		},
		{
			name:    "branch requiring escaping",
			source:  "https://github.com/AwayerCode/ReedHarness",
			version: "branch:feature/new skill",
			want:    "https://github.com/AwayerCode/ReedHarness/archive/refs/heads/feature/new%20skill.tar.gz",
		},
		{
			name:    "commit",
			source:  "https://github.com/AwayerCode/ReedHarness",
			version: "commit:0123456789abcdef",
			want:    "https://github.com/AwayerCode/ReedHarness/archive/0123456789abcdef.tar.gz",
		},
		{
			name:    "direct archive",
			source:  "https://example.com/content.tar.gz?token=one",
			version: "tag:v1",
			want:    "https://example.com/content.tar.gz?token=one",
		},
		{
			name:    "unqualified version",
			source:  "https://github.com/AwayerCode/ReedHarness",
			version: "main",
			wantErr: true,
		},
		{
			name:    "unsupported host",
			source:  "https://example.com/AwayerCode/ReedHarness",
			version: "branch:main",
			wantErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveArchiveURL(test.source, test.version)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestFetchSelectsPathsAndPreservesExecutableMode(t *testing.T) {
	t.Parallel()
	archive := testArchive(t, []testFile{
		{name: "._ReedHarness-main", body: "apple double", mode: 0o644},
		{name: "ReedHarness-main/skills/swe/SKILL.md", body: "skill", mode: 0o644},
		{name: "ReedHarness-main/skills/swe/scripts/check.py", body: "#!/usr/bin/env python3\n", mode: 0o755},
		{name: "ReedHarness-main/skills/other/SKILL.md", body: "other", mode: 0o644},
	})
	client := archiveClient(archive)

	destination := t.TempDir()
	err := Fetch(context.Background(), client, "https://example.test/archive.tar.gz", []string{"skills/swe"}, destination)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "skills", "swe", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "skill" {
		t.Fatalf("unexpected content %q", body)
	}
	info, err := os.Stat(filepath.Join(destination, "skills", "swe", "scripts", "check.py"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable mode, got %o", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(destination, "skills", "other")); !os.IsNotExist(err) {
		t.Fatalf("unselected path should not be extracted: %v", err)
	}
}

func TestFetchIgnoresTopLevelArchiveMetadata(t *testing.T) {
	t.Parallel()
	archive := testArchive(t, []testFile{
		{name: "pax_global_header", body: "github archive metadata", mode: 0o644},
		{name: "ReedHarness-main/skills/swe/SKILL.md", body: "skill", mode: 0o644},
	})

	destination := t.TempDir()
	err := Fetch(context.Background(), archiveClient(archive), "https://example.test/archive.tar.gz", []string{"skills/swe"}, destination)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "skills", "swe", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "skill" {
		t.Fatalf("unexpected content %q", body)
	}
}

func TestFetchSupportsRootlessArchive(t *testing.T) {
	t.Parallel()
	archive := testArchive(t, []testFile{
		{name: "skills/swe/SKILL.md", body: "skill", mode: 0o644},
		{name: "skills/swe/scripts/check.py", body: "#!/usr/bin/env python3\n", mode: 0o755},
	})

	destination := t.TempDir()
	err := Fetch(context.Background(), archiveClient(archive), "https://example.test/archive.tar.gz", []string{"skills/swe"}, destination)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "skills", "swe", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "skill" {
		t.Fatalf("unexpected content %q", body)
	}
	info, err := os.Stat(filepath.Join(destination, "skills", "swe", "scripts", "check.py"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable mode, got %o", info.Mode().Perm())
	}
}

func TestFetchSupportsSingleFileArchive(t *testing.T) {
	t.Parallel()
	archive := testArchive(t, []testFile{
		{name: "README.md", body: "# ReedHarness", mode: 0o644},
	})

	destination := t.TempDir()
	err := Fetch(context.Background(), archiveClient(archive), "https://example.test/archive.tar.gz", []string{"README.md"}, destination)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# ReedHarness" {
		t.Fatalf("unexpected content %q", body)
	}
}

func TestFetchRejectsUnsafeArchivePath(t *testing.T) {
	t.Parallel()
	archive := testArchive(t, []testFile{
		{name: "../outside", body: "bad", mode: 0o644},
	})
	err := Fetch(context.Background(), archiveClient(archive), "https://example.test/archive.tar.gz", []string{"skills/swe"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

func TestFetchRejectsSelectedSymlink(t *testing.T) {
	t.Parallel()
	archive := testArchive(t, []testFile{
		{name: "ReedHarness-main/skills/swe/link", linkname: "../../outside", mode: 0o777, typeflag: tar.TypeSymlink},
	})
	err := Fetch(context.Background(), archiveClient(archive), "https://example.test/archive.tar.gz", []string{"skills/swe"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a regular file or directory") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestFetchReportsMissingTarget(t *testing.T) {
	t.Parallel()
	archive := testArchive(t, []testFile{
		{name: "ReedHarness-main/skills/swe/SKILL.md", body: "skill", mode: 0o644},
	})
	err := Fetch(context.Background(), archiveClient(archive), "https://example.test/archive.tar.gz", []string{"skills/missing"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("expected missing target error, got %v", err)
	}
}

type testFile struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	linkname string
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func archiveClient(payload []byte) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Body:          io.NopCloser(bytes.NewReader(payload)),
			ContentLength: int64(len(payload)),
			Header:        make(http.Header),
			Request:       request,
		}, nil
	})}
}

func testArchive(t *testing.T, files []testFile) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		typeflag := file.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name:     file.name,
			Mode:     file.mode,
			Typeflag: typeflag,
			Linkname: file.linkname,
		}
		if typeflag == tar.TypeReg || typeflag == byte(0) {
			header.Size = int64(len(file.body))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if typeflag == tar.TypeReg || typeflag == byte(0) {
			if _, err := tarWriter.Write([]byte(file.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestFetchVerifiesGzipTrailer(t *testing.T) {
	valid := testArchive(t, []testFile{
		{name: "skills/swe/SKILL.md", body: "skill", mode: 0644},
		{name: "ignored/padding", body: strings.Repeat("x", 65536), mode: 0644},
	})
	corrupt := append([]byte(nil), valid...)
	corrupt[len(corrupt)-8] ^= 0xff
	for name, payload := range map[string][]byte{"valid": valid, "bad CRC": corrupt, "truncated trailer": valid[:len(valid)-8]} {
		t.Run(name, func(t *testing.T) {
			err := Fetch(context.Background(), archiveClient(payload), "https://example.test/archive.tar.gz", []string{"skills/swe"}, t.TempDir())
			if (err == nil) != (name == "valid") {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestFetchBoundsUnselectedContent(t *testing.T) {
	// Stream a highly compressible payload into a small archive, without a large allocation.
	var buffer bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(gz)
	for _, name := range []string{"ignored/first", "ignored/second"} {
		size := int64(maxExpandedBytes/2 + 1024)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: size}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(tw, zeroReader{}, size); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.WriteHeader(&tar.Header{Name: "skills/swe/SKILL.md", Mode: 0644, Size: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("skill")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	err = Fetch(context.Background(), archiveClient(buffer.Bytes()), "https://example.test/archive.tar.gz", []string{"skills/swe"}, destination)
	if err == nil || !strings.Contains(err.Error(), "expanded archive exceeds") {
		t.Fatalf("unbounded skipped content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "skills/swe/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("read beyond budget: %v", err)
	}
}

func TestFetchCountsUnselectedDirectories(t *testing.T) {
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for i := 0; i <= maxFiles; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: "ignored/", Mode: 0755, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	err := Fetch(context.Background(), archiveClient(buffer.Bytes()), "https://example.test/archive.tar.gz", []string{"skills/swe"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("unbounded skipped entries: %v", err)
	}
}
