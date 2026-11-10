package app

import (
	"errors"
	"flag"
	"github.com/AwayerCode/ReedHarness/apps/swe/internal/systemdesign"
	"io"
	"path/filepath"
)

type designBatch struct {
	Format  string                        `json:"format"`
	Mode    string                        `json:"mode"`
	Status  string                        `json:"status"`
	Issues  []systemdesign.DiscoveryIssue `json:"discovery_issues"`
	Scopes  []systemdesign.Scope          `json:"scopes"`
	Results []systemdesign.Report         `json:"results"`
}

func SystemDesign(args []string, out io.Writer) int {
	if len(args) > 0 && args[0] == "serve" {
		return systemDesignServe(args[1:], out)
	}
	if len(args) > 0 && args[0] == "preview" {
		return systemDesignPreview(args[1:], out)
	}
	if len(args) == 1 && helpFlag(args[0]) {
		return printHelp(out, designHelp+"\nFixed-address service: swe system-design serve --root <workspace> --expect-version <version>\n")
	}
	mode := ""
	if len(args) > 0 {
		mode = args[0]
	}
	fail := func(err error) int { return encode(out, systemdesign.UnavailableReport(mode, Version, err), 2) }
	if mode != "list" && mode != "validate" && mode != "check" && mode != "render" {
		return fail(errors.New("system-design requires list, validate, check or render"))
	}
	f := flag.NewFlagSet("system-design", flag.ContinueOnError)

	root := f.String("root", ".", "discovery root or base for a relative file")
	file := f.String("file", "", "system-design.yaml path; its directory is the scope")
	all := f.Bool("all", false, "check all discovered designs")
	profile := f.String("profile", "", "layers: partial design, validate only")
	showHelp := setHelp(f, out, "swe system-design "+mode+" [options]\n"+designHelp)
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return showHelp()
		}
		return fail(err)
	}
	if f.NArg() != 0 {
		return fail(errors.New("positional arguments are not supported"))
	}
	if *profile != "" && (*profile != "layers" || mode != "validate") {
		return fail(errors.New("--profile layers is only valid with validate"))
	}
	if mode == "list" {
		if *file != "" || *all || *profile != "" {
			return fail(errors.New("list accepts only --root"))
		}
	} else if (*file == "") == (!*all) || (mode == "render" && *all) {
		return fail(errors.New("choose exactly one of --file or --all; render requires --file"))
	}
	if mode == "list" || *all {
		session := systemdesign.NewSession()
		discovery, err := session.Discover(*root)
		if err != nil {
			return fail(err)
		}
		scopes := discovery.Scopes
		if len(scopes) == 0 && len(discovery.Issues) == 0 {
			return fail(errors.New("no system-design.yaml found"))
		}
		result := designBatch{Format: "swe-system-design-batch-v1", Mode: mode, Status: "passed", Scopes: scopes, Issues: discovery.Issues, Results: []systemdesign.Report{}}
		code := 0
		if len(discovery.Issues) > 0 {
			code = 2
		}
		if mode == "list" {
			for _, s := range scopes {
				if s.Unavailable != "" {
					code = 2
				}
			}
		}
		if mode != "list" {
			for _, s := range scopes {
				r, _, c := session.Execute(s, mode, *profile, Version)
				result.Results = append(result.Results, r)
				if c > code {
					code = c
				}
			}
		}
		if code == 1 {
			result.Status = "violations"
		}
		if code == 2 {
			result.Status = "unavailable"
		}
		return encode(out, result, code)
	}
	p := *file
	if !filepath.IsAbs(p) {
		p = filepath.Join(*root, p)
	}
	scope, err := systemdesign.FileScope(p)
	if err != nil {
		return fail(err)
	}
	r, html, code := systemdesign.Execute(scope, mode, *profile, Version)
	if mode == "render" && code == 0 {
		if _, err := out.Write(html); err != nil {
			return 2
		}
		return 0
	}
	return encode(out, r, code)
}
