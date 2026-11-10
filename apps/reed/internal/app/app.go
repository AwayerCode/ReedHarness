package app

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
)

var Version = "0.5.0-dev"

func Run(args []string, in io.Reader, out io.Writer) int {
	if len(args) > 1 && args[0] == "help" {
		return Run(append(append([]string{}, args[1:]...), "--help"), in, out)
	}
	if len(args) > 0 && args[0] == "skills" {
		return Skills(args[1:], out)
	}
	if len(args) > 0 && args[0] == "system-design" {
		return SystemDesign(args[1:], out)
	}
	if len(args) > 0 && args[0] == "scope" {
		return Scope(args[1:], out)
	}
	if len(args) > 0 && slices.Contains([]string{"documents", "design", "artifacts", "structure"}, args[0]) {
		return Direct(args[0], args[1:], out)
	}
	if len(args) == 0 || len(args) == 1 && (args[0] == "help" || helpFlag(args[0])) {
		_, err := fmt.Fprintln(out, "reed check --request <file|-> [--format json]\nreed version\nreed skills list; reed skills read <name> [relative-resource-path]\nGo core: documents, design, artifacts, structure; scope snapshot/check/readonly; system-design list --root <dir>; system-design validate/check --file <system-design.yaml> or --root <dir> --all; system-design render --file <system-design.yaml>; system-design preview --file <system-design.yaml> --expect-version <version>. No generic extension protocol.")
		if err != nil {
			return 2
		}
		return 0
	}
	if len(args) == 1 && args[0] == "version" {
		return encode(out, map[string]any{"version": Version, "protocolVersion": ProtocolVersion, "runtime": runtime.Version(), "standalone": true}, 0)
	}
	if args[0] != "check" {
		return unavailable(out, errors.New("unknown command"))
	}
	return runCheck(args, in, out)
}
