package app

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"path/filepath"
)

// Convenience commands build the same request as JSON callers, with no second checker implementation.
func Direct(command string, args []string, out io.Writer) int {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	root := f.String("root", ".", "project root")
	var kind, file, profile, system, protocol, design string
	var scopes values
	switch command {
	case "documents":
		f.StringVar(&kind, "kind", "", "business-model, system-design, interface-protocol, interaction-standards or test-design")
		f.StringVar(&file, "file", "", "document path")
		f.StringVar(&profile, "profile", "", "architecture: partial system design")
	case "design":
		f.StringVar(&system, "system-design", "", "system design document")
		f.StringVar(&protocol, "interface-protocol", "", "interface protocol document")
		f.StringVar(&protocol, "protocol", "", "alias for --interface-protocol")
	case "artifacts":
		f.StringVar(&kind, "kind", "", "ui or test")
		f.StringVar(&file, "file", "", "artifact document path")
		f.StringVar(&protocol, "interface-protocol", "", "interface protocol document")
		f.StringVar(&protocol, "protocol", "", "alias for --interface-protocol")
	case "structure":
		f.StringVar(&design, "design", "", "system design document")
		f.Var(&scopes, "scope", "relative scope (repeatable)")
	}
	showHelp := setHelp(f, out, "reed "+command+" [options]")
	if e := f.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return showHelp()
		}
		return unavailable(out, e)
	}
	if f.NArg() != 0 {
		return unavailable(out, errors.New("unexpected positional arguments"))
	}
	absolute, e := filepath.Abs(*root)
	if e != nil {
		return unavailable(out, e)
	}
	absolute, e = filepath.EvalSymlinks(absolute)
	if e != nil {
		return unavailable(out, e)
	}
	input := map[string]any{}
	switch command {
	case "documents":
		input = map[string]any{"kind": kind, "file": file, "profile": profile}
	case "design":
		input = map[string]any{"systemDesign": system, "protocol": protocol}
	case "artifacts":
		input = map[string]any{"kind": kind, "file": file, "protocol": protocol}
	case "structure":
		input = map[string]any{"design": design, "scopes": []string(scopes)}
	}
	raw, _ := json.Marshal(input)
	required := true
	req := Request{ProtocolVersion: ProtocolVersion, TaskID: command, Round: 1, Mode: "review", ProjectRoot: absolute, Checks: []CheckRequest{{CheckID: command, Required: &required, Input: raw}}}
	data, _ := json.Marshal(req)
	return runRequest(data, out)
}
