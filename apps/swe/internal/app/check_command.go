package app

import (
	"errors"
	"io"
	"os"
	"slices"
)

const maxRequest = 1024 * 1024

func runCheck(args []string, in io.Reader, out io.Writer) int {
	if len(args) == 2 && helpFlag(args[1]) {
		return printHelp(out, checkHelp)
	}
	options := map[string]string{}
	for i := 1; i < len(args); i += 2 {
		if !slices.Contains([]string{"--request", "--format"}, args[i]) || options[args[i]] != "" || i+1 >= len(args) {
			return unavailable(out, errors.New("invalid command options"))
		}
		options[args[i]] = args[i+1]
	}
	if options["--request"] == "" || options["--format"] != "" && options["--format"] != "json" {
		return unavailable(out, errors.New("use --request <file|-> --format json"))
	}
	source := in
	if options["--request"] != "-" {
		file, err := os.Open(options["--request"])
		if err != nil {
			return unavailable(out, err)
		}
		defer file.Close()
		source = file
	}
	data, err := io.ReadAll(io.LimitReader(source, maxRequest+1))
	if err != nil {
		return unavailable(out, err)
	}
	if len(data) > maxRequest {
		return unavailable(out, errors.New("request exceeds 1 MiB"))
	}
	return runRequest(data, out)
}

func runRequest(data []byte, out io.Writer) int {
	var req Request
	if err := strict(data, &req); err != nil {
		return unavailable(out, err)
	}
	root, err := validateRequest(req)
	if err != nil {
		return unavailable(out, err)
	}
	result, code, err := executeRequest(req, root)
	if err != nil {
		return unavailable(out, err)
	}
	return encode(out, result, code)
}
