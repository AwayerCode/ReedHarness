package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestVersionAndHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	runner := New(Options{Stdout: &stdout, Stderr: &stderr, Version: "v1.2.3"})
	if code := runner.Run(context.Background(), []string{"version"}); code != 0 {
		t.Fatalf("version exit code %d", code)
	}
	if stdout.String() != "reed v1.2.3\n" {
		t.Fatalf("unexpected output %q", stdout.String())
	}

	stdout.Reset()
	if code := runner.Run(context.Background(), []string{"help"}); code != 0 {
		t.Fatalf("help exit code %d", code)
	}
	if !strings.Contains(stdout.String(), "install") {
		t.Fatalf("help does not describe install: %q", stdout.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	runner := New(Options{Stdout: &stdout, Stderr: &stderr})
	if code := runner.Run(context.Background(), []string{"unknown"}); code != 2 {
		t.Fatalf("got exit code %d", code)
	}
	if !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("unexpected error %q", stderr.String())
	}
}

func TestInstallRequiresArguments(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	runner := New(Options{Stdout: &stdout, Stderr: &stderr})
	if code := runner.Run(context.Background(), []string{"install"}); code != 2 {
		t.Fatalf("got exit code %d", code)
	}
	if !strings.Contains(stderr.String(), "requires --source") {
		t.Fatalf("unexpected error %q", stderr.String())
	}
}

func TestRemovedManagementCommands(t *testing.T) {
	for _, command := range []string{"update", "status", "remove"} {
		var stderr bytes.Buffer
		runner := New(Options{Stderr: &stderr})
		if code := runner.Run(context.Background(), []string{command}); code != 2 {
			t.Fatalf("%s exit %d", command, code)
		}
		if !strings.Contains(stderr.String(), "unknown command") {
			t.Fatal(stderr.String())
		}
	}
}

func TestSearchAcceptsQueryBeforeFlags(t *testing.T) {
	t.Parallel()
	reordered := floatFlagsFirst([]string{"review", "--source", "https://example.test", "--version", "tag:v1"})
	want := []string{"--source", "https://example.test", "--version", "tag:v1", "review"}
	if strings.Join(reordered, " ") != strings.Join(want, " ") {
		t.Fatalf("got %#v", reordered)
	}

	equals := floatFlagsFirst([]string{"review", "--source=https://example.test"})
	want = []string{"--source=https://example.test", "review"}
	if strings.Join(equals, " ") != strings.Join(want, " ") {
		t.Fatalf("got %#v", equals)
	}
}
