package app

import (
	"errors"
	"flag"
	"github.com/AwayerCode/ReedHarness/apps/swe-cli/internal/workspace"
	"io"
	"strings"
)

type values []string

func (v *values) String() string     { return strings.Join(*v, ",") }
func (v *values) Set(s string) error { *v = append(*v, s); return nil }
func Scope(args []string, out io.Writer) int {
	if len(args) == 1 && helpFlag(args[0]) {
		return printHelp(out, scopeHelp)
	}
	if len(args) == 0 {
		return unavailable(out, errors.New("scope requires snapshot, check or readonly"))
	}
	mode := args[0]
	if mode != "snapshot" && mode != "check" && mode != "readonly" {
		return unavailable(out, errors.New("unknown scope mode"))
	}
	f := flag.NewFlagSet("scope", flag.ContinueOnError)

	root := f.String("root", "", "Git root")
	baseline := f.String("baseline", "", "external baseline")
	expected := f.String("expected", "", "versioned fingerprint")
	var files, dirs values
	f.Var(&files, "allow-file", "writable file")
	f.Var(&dirs, "allow-dir", "writable directory")
	showHelp := setHelp(f, out, "swe-cli scope "+mode+" [options]\n"+scopeHelp)
	if e := f.Parse(args[1:]); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return showHelp()
		}
		return unavailable(out, e)
	}
	if f.NArg() != 0 || *root == "" {
		return unavailable(out, errors.New("scope requires --root and no positional arguments"))
	}
	r, e := workspace.Root(*root)
	if e != nil {
		return unavailable(out, e)
	}
	switch mode {
	case "readonly":
		if *baseline != "" || len(files)+len(dirs) > 0 {
			return unavailable(out, errors.New("readonly cannot accept baseline or writable scope"))
		}
		if *expected != "" {
			if e := workspace.ValidateFingerprint(*expected); e != nil {
				return unavailable(out, e)
			}
		}
		hash, count, e := workspace.Fingerprint(r)
		if e != nil {
			return unavailable(out, e)
		}
		changed := *expected != "" && *expected != hash
		code := 0
		if changed {
			code = 1
		}
		return encode(out, map[string]any{"format": workspace.Format, "sha256": hash, "files": count, "changed": changed, "baseline": *expected == "", "coverage": workspace.Coverage}, code)
	case "snapshot":
		if *expected != "" || *baseline == "" {
			return unavailable(out, errors.New("snapshot requires --baseline and no --expected"))
		}
		if e := workspace.Snapshot(r, *baseline, files, dirs); e != nil {
			return unavailable(out, e)
		}
		return encode(out, map[string]any{"format": workspace.Format, "baseline": "created", "coverage": workspace.Coverage}, 0)
	case "check":
		if *expected != "" || *baseline == "" || len(files)+len(dirs) > 0 {
			return unavailable(out, errors.New("check uses only the original baseline and cannot expand scope"))
		}
		changed, unexpected, e := workspace.Check(r, *baseline)
		if e != nil {
			return unavailable(out, e)
		}
		code := 0
		if len(unexpected) > 0 {
			code = 1
		}
		return encode(out, map[string]any{"format": workspace.Format, "changed": changed, "unexpected": unexpected, "coverage": workspace.Coverage}, code)
	default:
		return unavailable(out, errors.New("unknown scope mode"))
	}
}
