package app

import (
	"flag"
	"fmt"
	"io"
)

func helpFlag(value string) bool { return value == "--help" || value == "-h" }

func printHelp(out io.Writer, text string) int {
	if _, err := fmt.Fprintln(out, text); err != nil {
		return 2
	}
	return 0
}

type helpWriter struct {
	io.Writer
	err error
}

func (w *helpWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err != nil {
		w.err = err
	}
	return n, err
}

func setHelp(f *flag.FlagSet, out io.Writer, usage string) func() int {
	// Parse failures stay in the structured result; only explicit help is text.
	f.SetOutput(io.Discard)
	f.Usage = func() {}
	return func() int {
		w := &helpWriter{Writer: out}
		fmt.Fprintln(w, "Usage: "+usage)
		f.SetOutput(w)
		f.PrintDefaults()
		f.SetOutput(io.Discard)
		if w.err != nil {
			return 2
		}
		return 0
	}
}

const scopeHelp = `Usage: reed scope <snapshot|check|readonly> [options]
  snapshot --root <Git root> --baseline <new external file> --allow-file <path> or --allow-dir <path>
  check --root <Git root> --baseline <original file>
  readonly --root <Git root> [--expected <fingerprint>]`

const designHelp = `Usage: reed system-design <list|validate|check|render|preview> [options]
  list --root <dir>
  validate --file <system-design.yaml> or --root <dir> --all [--profile layers]
  check --file <system-design.yaml> or --root <dir> --all
  render --file <system-design.yaml>
  preview --file <system-design.yaml> --expect-version <version>`

const checkHelp = `Usage: reed check --request <file|-> [--format json]
  --request <file|->  JSON protocol v2 request; - reads stdin
  --format json       Structured JSON result (default)
Exit codes: 0 passed, 1 violations, 2 unavailable.`
