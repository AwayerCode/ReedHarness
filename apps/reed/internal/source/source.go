package source

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxArchiveBytes   = 128 << 20
	maxFileBytes      = 32 << 20
	maxExtractedBytes = 512 << 20
	maxExpandedBytes  = 512 << 20
	maxFiles          = 100000
)

func ResolveArchiveURL(rawSource, version string) (string, error) {
	if version == "" {
		return "", errors.New("version is required")
	}
	sourceURL, err := url.Parse(rawSource)
	if err != nil || sourceURL.Scheme == "" || sourceURL.Host == "" {
		return "", fmt.Errorf("invalid source URL %q", rawSource)
	}
	if sourceURL.Scheme != "https" && sourceURL.Scheme != "http" {
		return "", fmt.Errorf("unsupported source scheme %q", sourceURL.Scheme)
	}
	if strings.HasSuffix(sourceURL.Path, ".tar.gz") {
		return sourceURL.String(), nil
	}
	host := strings.ToLower(sourceURL.Host)
	if host != "github.com" && host != "www.github.com" {
		return "", errors.New("direct archive URL or GitHub repository URL is required")
	}

	parts := strings.Split(strings.Trim(sourceURL.Path, "/"), "/")
	repositoryName := ""
	if len(parts) == 2 {
		repositoryName = strings.TrimSuffix(parts[1], ".git")
	}
	if len(parts) != 2 || parts[0] == "" || repositoryName == "" {
		return "", fmt.Errorf("GitHub source must identify one repository: %q", rawSource)
	}
	repository := parts[0] + "/" + repositoryName

	var refPath string
	switch {
	case strings.HasPrefix(version, "branch:"):
		refPath = "refs/heads/" + strings.TrimPrefix(version, "branch:")
	case strings.HasPrefix(version, "tag:"):
		refPath = "refs/tags/" + strings.TrimPrefix(version, "tag:")
	case strings.HasPrefix(version, "commit:"):
		refPath = strings.TrimPrefix(version, "commit:")
	default:
		return "", fmt.Errorf("version %q must use branch:, tag: or commit: prefix", version)
	}
	if refPath == "" || strings.HasSuffix(refPath, "/") {
		return "", fmt.Errorf("invalid version %q", version)
	}

	archiveURL := &url.URL{
		Scheme: sourceURL.Scheme,
		Host:   host,
		Path:   "/" + repository + "/archive/" + refPath + ".tar.gz",
	}
	return archiveURL.String(), nil
}

func Fetch(ctx context.Context, client *http.Client, archiveURL string, targets []string, destination string) error {
	return FetchOptional(ctx, client, archiveURL, targets, nil, destination)
}

// FetchOptional extracts required and optional paths from the same verified
// archive snapshot. Missing optional paths are allowed; malformed entries are not.
func FetchOptional(ctx context.Context, client *http.Client, archiveURL string, targets, optional []string, destination string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return fmt.Errorf("create archive request: %w", err)
	}
	request.Header.Set("User-Agent", "reed")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download archive: HTTP %s", response.Status)
	}
	if response.ContentLength > maxArchiveBytes {
		return fmt.Errorf("archive exceeds %d bytes", maxArchiveBytes)
	}

	limitedBody := &io.LimitedReader{R: response.Body, N: maxArchiveBytes + 1}
	gzipReader, err := gzip.NewReader(limitedBody)
	if err != nil {
		return fmt.Errorf("open tar.gz archive: %w", err)
	}
	defer gzipReader.Close()

	selected := make(map[string]bool, len(targets))
	for _, target := range targets {
		selected[target] = true
	}
	for _, target := range optional {
		selected[target] = true
	}
	fileCount := 0
	var extractedBytes int64

	// Bound the entire decompressed stream, including skipped entries and padding.
	expanded := &io.LimitedReader{R: gzipReader, N: maxExpandedBytes + 1}
	reader := tar.NewReader(expanded)
	entryCount := 0
	for {
		header, err := reader.Next()
		if expanded.N == 0 {
			return fmt.Errorf("expanded archive exceeds %d bytes", maxExpandedBytes)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		entryCount++
		if entryCount > maxFiles {
			return fmt.Errorf("archive contains more than %d entries", maxFiles)
		}
		// Reject oversized skipped payloads before tar.Next decompresses them.
		if header.Size > expanded.N-1 {
			return fmt.Errorf("expanded archive exceeds %d bytes", maxExpandedBytes)
		}
		name, err := cleanArchiveName(header.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		if isArchiveMetadata(name) {
			continue
		}

		relative := selectedArchivePath(name, selected)
		if !isSelected(relative, selected) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(filepath.Join(destination, filepath.FromSlash(relative)), 0o755); err != nil {
				return fmt.Errorf("create archive directory: %w", err)
			}
		case tar.TypeReg, 0: // 0 is the legacy regular-file flag still present in old archives.
			fileCount++
			if fileCount > maxFiles {
				return fmt.Errorf("archive contains more than %d selected files", maxFiles)
			}
			if header.Size < 0 || header.Size > maxFileBytes {
				return fmt.Errorf("archive file %q exceeds %d bytes", relative, maxFileBytes)
			}
			extractedBytes += header.Size
			if extractedBytes > maxExtractedBytes {
				return fmt.Errorf("selected content exceeds %d bytes", maxExtractedBytes)
			}
			if err := writeArchiveFile(destination, relative, header, reader); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive path %q is not a regular file or directory", relative)
		}
	}

	// tar EOF precedes the gzip trailer. Read through it to verify CRC and size.
	_, streamErr := io.Copy(io.Discard, expanded)
	if expanded.N == 0 {
		return fmt.Errorf("expanded archive exceeds %d bytes", maxExpandedBytes)
	}
	if streamErr != nil {
		return fmt.Errorf("verify gzip stream: %w", streamErr)
	}
	if limitedBody.N == 0 {
		return fmt.Errorf("archive exceeds %d bytes", maxArchiveBytes)
	}
	for _, target := range targets {
		if _, err := os.Lstat(filepath.Join(destination, filepath.FromSlash(target))); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("resource path %q was not found in the archive", target)
			}
			return fmt.Errorf("inspect downloaded resource %q: %w", target, err)
		}
	}
	return nil
}

func writeArchiveFile(destination, relative string, header *tar.Header, reader io.Reader) error {
	target := filepath.Join(destination, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create directory for %q: %w", relative, err)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create archive file %q: %w", relative, err)
	}
	_, copyErr := io.CopyN(file, reader, header.Size)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("extract archive file %q: %w", relative, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close archive file %q: %w", relative, closeErr)
	}
	mode := os.FileMode(header.Mode).Perm()
	if mode == 0 {
		mode = 0o644
	}
	if err := os.Chmod(target, mode); err != nil {
		return fmt.Errorf("set archive file permissions for %q: %w", relative, err)
	}
	return nil
}

func isSelected(relative string, selected map[string]bool) bool {
	for target := range selected {
		if relative == target || strings.HasPrefix(relative, target+"/") {
			return true
		}
	}
	return false
}

func selectedArchivePath(name string, selected map[string]bool) string {
	if isSelected(name, selected) {
		return name
	}
	if _, stripped, found := strings.Cut(name, "/"); found && isSelected(stripped, selected) {
		return stripped
	}
	return name
}

func cleanArchiveName(name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	if name == "" || name == "." {
		return "", nil
	}
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func isArchiveMetadata(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == ".DS_Store" || strings.HasPrefix(part, "._") {
			return true
		}
	}
	return name == "pax_global_header" || strings.HasPrefix(name, "pax_global_header/") ||
		name == "__MACOSX" || strings.HasPrefix(name, "__MACOSX/")
}
