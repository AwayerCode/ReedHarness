// skillpackage builds independent text Skills and platform CLI archives.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

var platforms = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}

// Skill packages come from the repository directory layout; there is no separate
// registry to keep in sync.
func skillNames(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "skills"))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, "skills", entry.Name(), "SKILL.md")); err != nil || !info.Mode().IsRegular() {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil, errors.New("no Skill directory with SKILL.md found")
	}
	sort.Strings(names)
	return names, nil
}

func main() {
	version := flag.String("version", "", "independent CLI version")
	dist := flag.String("dist", "dist/bin", "directory containing the six built CLIs")
	root := flag.String("root", "../..", "repository root")
	out := flag.String("out", "dist", "archive output directory")
	flag.Parse()
	if err := buildPackages(*root, *dist, *out, *version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func buildPackages(root, dist, out, version string) error {
	if strings.TrimSpace(version) == "" {
		return errors.New("explicit CLI version required")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	names, err := skillNames(root)
	if err != nil {
		return err
	}
	var archives []string
	for _, name := range names {
		destination := filepath.Join(out, name+".tar.gz")
		if err := buildSkillPackage(filepath.Join(root, "skills", name), destination); err != nil {
			return err
		}
		archives = append(archives, destination)
	}
	for _, platform := range platforms {
		suffix := strings.ReplaceAll(platform, "/", "_")
		executable := "swe"
		binary := filepath.Join(dist, "swe_"+suffix)
		if strings.HasPrefix(platform, "windows/") {
			executable += ".exe"
			binary += ".exe"
		}
		destination := filepath.Join(out, "swe_"+suffix+".tar.gz")
		if err := buildCLIPackage(binary, destination, version, platform, executable); err != nil {
			return fmt.Errorf("package %s: %w", platform, err)
		}
		archives = append(archives, destination)
	}
	var sums []string
	for _, archive := range archives {
		data, err := os.ReadFile(archive)
		if err != nil {
			return err
		}
		sums = append(sums, fmt.Sprintf("%x  %s", sha256.Sum256(data), filepath.Base(archive)))
	}
	sort.Strings(sums)
	return os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0644)
}

type archiveEntry struct {
	name string
	mode int64
	data []byte
}

func buildSkillPackage(root, destination string) error {
	if info, err := os.Stat(filepath.Join(root, "SKILL.md")); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Skill entry missing or invalid: %s", root)
	}
	var entries []archiveEntry
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if relative == "bin" && entry.IsDir() {
			return filepath.SkipDir
		}
		if relative == "runtime.json" || relative == "bin" || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular Skill source %q", name)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return fmt.Errorf("non-text Skill source %q", name)
		}
		entries = append(entries, archiveEntry{"skills/" + filepath.Base(root) + "/" + filepath.ToSlash(relative), 0644, data})
		return nil
	})
	if err != nil {
		return err
	}
	return writeArchive(destination, entries)
}

func buildCLIPackage(binary, destination, version, platform, executable string) error {
	data, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	binding, err := json.MarshalIndent(map[string]any{
		"schema_version": 1, "name": "swe", "version": version,
		"assets": map[string]any{platform: map[string]string{"sha256": fmt.Sprintf("%x", sha256.Sum256(data))}},
	}, "", "  ")
	if err != nil {
		return err
	}
	return writeArchive(destination, []archiveEntry{{"swe/runtime.json", 0644, append(binding, '\n')}, {"swe/bin/" + executable, 0755, data}})
}

func writeArchive(destination string, entries []archiveEntry) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".package-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	defer file.Close()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.data)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(entry.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := file.Chmod(0644); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, destination)
}
