package app

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBootstrapRunsTemporarilyAndPropagatesFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX bootstrap; Windows uses the release executable")
	}
	for _, scenario := range []struct {
		name    string
		exit    string
		badHash bool
	}{
		{"success", "0", false}, {"command failure", "7", false}, {"bad checksum", "0", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			project := filepath.Join(root, "project")
			scratch := filepath.Join(root, "scratch")
			for _, dir := range []string{bin, project, scratch} {
				if err := os.Mkdir(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			payload := []byte("#!/bin/sh\nprintf '%s\\n' \"$0\" \"$PWD\" \"$@\" > \"$REED_TEST_LOG\"\nexit \"$REED_TEST_EXIT\"\n")
			payloadPath := filepath.Join(root, "payload")
			if err := os.WriteFile(payloadPath, payload, 0644); err != nil {
				t.Fatal(err)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(payload))
			if scenario.badHash {
				digest = strings.Repeat("0", 64)
			}
			var sums strings.Builder
			for _, platform := range []string{"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64"} {
				fmt.Fprintf(&sums, "%s  reed_%s\n", digest, platform)
			}
			sumsPath := filepath.Join(root, "sums")
			if err := os.WriteFile(sumsPath, []byte(sums.String()), 0644); err != nil {
				t.Fatal(err)
			}
			curl := `#!/bin/sh
url="$2"
destination="$4"
case "$url" in
 */SHA256SUMS) cp "$REED_TEST_SUMS" "$destination" ;;
 *) cp "$REED_TEST_PAYLOAD" "$destination" ;;
esac
`
			if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(curl), 0755); err != nil {
				t.Fatal(err)
			}
			script, err := filepath.Abs("../../../../install.sh")
			if err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(root, "invocation")
			cmd := exec.Command("sh", script, "--root", project, "--", "install", "--path", "skills/a b")
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TMPDIR="+scratch, "REED_TEST_PAYLOAD="+payloadPath, "REED_TEST_SUMS="+sumsPath, "REED_TEST_LOG="+log, "REED_TEST_EXIT="+scenario.exit)
			output, err := cmd.CombinedOutput()
			want := 0
			if scenario.badHash {
				want = 1
			} else if scenario.exit == "7" {
				want = 7
			}
			if cmd.ProcessState.ExitCode() != want {
				t.Fatalf("exit: %v, output: %s", err, output)
			}
			data, readErr := os.ReadFile(log)
			if scenario.badHash {
				if !os.IsNotExist(readErr) {
					t.Fatal("executed unverified payload")
				}
			} else {
				if readErr != nil {
					t.Fatal(readErr)
				}
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				if len(lines) != 5 || lines[1] != project || strings.Join(lines[2:], "|") != "install|--path|skills/a b" {
					t.Fatalf("arguments: %q", data)
				}
				if _, err := os.Stat(lines[0]); !os.IsNotExist(err) {
					t.Fatalf("temporary executable retained: %v", err)
				}
			}
			for _, dir := range []string{project, scratch} {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("left files in %s: %v %v", dir, entries, err)
				}
			}
		})
	}
}
