package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AwayerCode/ReedHarness/apps/reed/internal/installer"
)

type Options struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
	Client  *http.Client
}

type Runner struct {
	stdout  io.Writer
	stderr  io.Writer
	version string
	client  *http.Client
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func New(options Options) *Runner {
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	return &Runner{
		stdout:  options.Stdout,
		stderr:  options.Stderr,
		version: options.Version,
		client:  options.Client,
	}
}

func (r *Runner) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		r.printUsage(r.stderr)
		return 2
	}
	switch args[0] {
	case "install":
		return r.install(ctx, args[1:])
	case "list":
		return r.list(args[1:])
	case "version":
		if len(args) != 1 {
			fmt.Fprintln(r.stderr, "reed: version does not accept arguments")
			return 2
		}
		fmt.Fprintf(r.stdout, "reed %s\n", r.version)
		return 0
	case "help", "-h", "--help":
		r.printUsage(r.stdout)
		return 0
	default:
		fmt.Fprintf(r.stderr, "reed: unknown command %q\n\n", args[0])
		r.printUsage(r.stderr)
		return 2
	}
}

func (r *Runner) install(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	var paths stringList
	var rawSource string
	var version string
	var root string
	var dryRun bool
	flags.Var(&paths, "path", "repository-relative Skill directory (repeatable)")
	flags.StringVar(&rawSource, "source", "", "ReedHarness repository or direct .tar.gz URL")
	flags.StringVar(&version, "version", "", "branch:<name>, tag:<name>, or commit:<sha>")
	flags.StringVar(&root, "root", "", "project root (default: current directory)")
	flags.BoolVar(&dryRun, "dry-run", false, "resolve and verify the download without writing files")
	flags.Usage = func() { r.printInstallUsage(flags.Output()) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(r.stderr, "reed: unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if rawSource == "" || version == "" || len(paths) == 0 {
		fmt.Fprintln(r.stderr, "reed: install requires --source, --version, and at least one --path")
		return 2
	}

	projectRoot, err := resolveRoot(root)
	if err != nil {
		r.printError(err)
		return 1
	}
	manager := installer.Manager{ProjectRoot: projectRoot, Client: r.client, DryRun: dryRun}
	results, err := manager.Install(ctx, rawSource, version, paths)
	if err != nil {
		r.printError(err)
		return 1
	}
	for _, result := range results {
		action := "downloaded"
		if dryRun {
			action = "would download"
		}
		fmt.Fprintf(r.stdout, "%s %s at %s (%d files)\n", action, result.Path, result.Version, result.Files)
	}
	return 0
}

func (r *Runner) list(args []string) int {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	var root string
	flags.StringVar(&root, "root", "", "project root (default: current directory)")
	flags.Usage = func() { r.printListUsage(flags.Output()) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(r.stderr, "reed: unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	projectRoot, err := resolveRoot(root)
	if err != nil {
		r.printError(err)
		return 1
	}
	entries, err := installer.Manager{ProjectRoot: projectRoot}.List()
	if err != nil {
		r.printError(err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Fprintln(r.stdout, "no installed skills")
		return 0
	}
	fmt.Fprintf(r.stdout, "%-28s %-40s %s\n", "NAME", "PATH", "SUMMARY")
	for _, entry := range entries {
		fmt.Fprintf(r.stdout, "%-28s %-40s %s\n", entry.Name, entry.Path, entry.Summary)
	}
	return 0
}

func (r *Runner) printError(err error) {
	fmt.Fprintf(r.stderr, "reed: %v\n", err)
}

func (r *Runner) printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: reed <command> [options]")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Commands:")
	fmt.Fprintln(writer, "  install   Install selected Skills")
	fmt.Fprintln(writer, "  list      List Skills found in .agents/skills/")
	fmt.Fprintln(writer, "  version   Print the ReedHarness version")
	fmt.Fprintln(writer, "\nSkills install to .agents/skills/. No registry, project records or locks.")
}

func (r *Runner) printInstallUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: reed install --source <url> --version <ref> --path <path> [--path <path>...] [--dry-run] [--root <dir>]")
}

func (r *Runner) printListUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: reed list [--root <dir>]")
}

func resolveRoot(root string) (string, error) {
	if root == "" {
		root = "."
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect project root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root %q is not a directory", absolute)
	}
	return absolute, nil
}
