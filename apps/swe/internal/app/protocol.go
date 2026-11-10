package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Version 2 deliberately rejects the removed extension protocol.
const ProtocolVersion = 2

type Input struct {
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Profile string `json:"profile,omitempty"`
}
type CheckRequest struct {
	CheckID  string          `json:"checkId"`
	Required *bool           `json:"required"`
	Input    json.RawMessage `json:"input"`
}
type Request struct {
	ProtocolVersion int            `json:"protocolVersion"`
	TaskID          string         `json:"taskId"`
	Round           int64          `json:"round"`
	Mode            string         `json:"mode"`
	ProjectRoot     string         `json:"projectRoot"`
	Checks          []CheckRequest `json:"checks"`
}
type Diagnostic struct {
	RuleID   string `json:"ruleId"`
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}
type Result struct {
	CheckID     string       `json:"checkId"`
	Source      string       `json:"source"`
	Required    bool         `json:"required"`
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Coverage    string       `json:"coverage"`
	Evidence    []string     `json:"evidence"`
	Unverified  []string     `json:"unverified,omitempty"`
}
type Summary struct {
	Status   string   `json:"status"`
	ExitCode int      `json:"exitCode"`
	Warnings []string `json:"warnings"`
}

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func strict(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("request must contain exactly one JSON value")
	}
	return nil
}
func encode(out io.Writer, v any, code int) int {
	if json.NewEncoder(out).Encode(v) != nil {
		return 2
	}
	return code
}
func unavailable(out io.Writer, err error) int {
	return encode(out, map[string]any{"protocolVersion": ProtocolVersion, "error": Diagnostic{RuleID: "request.unavailable", Message: err.Error()}, "summary": Summary{Status: "unavailable", ExitCode: 2, Warnings: []string{}}}, 2)
}
func validateRequest(req Request) (string, error) {
	if req.ProtocolVersion != ProtocolVersion {
		return "", fmt.Errorf("unsupported protocolVersion; use %d (extensions removed)", ProtocolVersion)
	}
	if strings.TrimSpace(req.TaskID) == "" || req.Round < 1 || req.Round > 9007199254740991 || !slices.Contains([]string{"design", "implement", "test", "review"}, req.Mode) || !filepath.IsAbs(req.ProjectRoot) {
		return "", errors.New("invalid task, round, mode or absolute projectRoot")
	}
	root, err := filepath.EvalSymlinks(req.ProjectRoot)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("projectRoot must be a directory")
	}
	ids := map[string]bool{}
	required := false
	for _, c := range req.Checks {
		if strings.TrimSpace(c.CheckID) == "" || ids[c.CheckID] || c.Required == nil || len(bytes.TrimSpace(c.Input)) == 0 || bytes.TrimSpace(c.Input)[0] != '{' {
			return "", errors.New("invalid or duplicate checkId, required or input")
		}
		ids[c.CheckID] = true
		required = required || *c.Required
	}
	if !required {
		return "", errors.New("at least one required check is needed")
	}
	return root, nil
}
