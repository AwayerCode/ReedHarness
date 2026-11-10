package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/AwayerCode/ReedHarness/apps/reed/internal/checks"
	"github.com/AwayerCode/ReedHarness/apps/reed/internal/workspace"
	"path/filepath"
	"slices"
	"strings"
)

func executeRequest(req Request, root string) (map[string]any, int, error) {
	var schemas map[string]checks.Schema
	if err := json.Unmarshal([]byte(checks.SchemaJSON), &schemas); err != nil {
		return nil, 2, err
	}
	reader := &workspace.Observer{Root: root, Files: map[string]string{}}
	results := []Result{}
	for _, c := range req.Checks {
		r := Result{CheckID: c.CheckID, Source: "core", Required: *c.Required, Status: "unavailable", Diagnostics: []Diagnostic{}, Evidence: []string{}, Coverage: "Only requested checks; not task completion"}
		if slices.Contains([]string{"design", "artifacts", "structure"}, c.CheckID) {
			report, err := related(c.CheckID, c.Input, root, schemas, reader)
			if err != nil {
				r.Diagnostics = append(r.Diagnostics, Diagnostic{RuleID: c.CheckID + ".unavailable", Message: err.Error()})
			} else {
				r.Status = "passed"
				if len(report.Errors) > 0 {
					r.Status = "failed"
				}
				for _, message := range report.Errors {
					r.Diagnostics = append(r.Diagnostics, Diagnostic{RuleID: c.CheckID + ".relation", Message: message})
				}
				r.Evidence = report.Checked
				r.Unverified = report.Unverified
				r.Coverage = "Explicit relations only; unverified items require manual review"
			}
		} else if c.CheckID != "documents" {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{RuleID: "check.unavailable", Message: "Check not implemented: " + c.CheckID})
		} else {
			var input Input
			err := strict(c.Input, &input)
			schema, ok := schemas[input.Kind]
			if err == nil && (!ok || strings.TrimSpace(input.File) == "" || input.Profile != "" && input.Profile != "architecture") {
				err = errors.New("invalid documents input")
			}
			if err == nil {
				file := input.File
				if !filepath.IsAbs(file) {
					file = filepath.Join(root, file)
				}
				var problems []string
				problems, err = checks.Validate(file, input.Kind, schema, reader, input.Profile)
				if err == nil {
					r.Status = "passed"
					if len(problems) > 0 {
						r.Status = "failed"
					}
					for _, message := range problems {
						r.Diagnostics = append(r.Diagnostics, Diagnostic{RuleID: "documents.format", Message: message, Location: file})
					}
					r.Coverage = "Document format and local links; not semantic correctness"
					if input.Profile != "" {
						r.Coverage = "Partial:" + input.Profile + "; not full design validation"
					}
					r.Evidence = append(r.Evidence, file)
				}
			}
			if err != nil {
				r.Diagnostics = append(r.Diagnostics, Diagnostic{RuleID: "check.unavailable", Message: err.Error()})
			}
		}
		results = append(results, r)
	}
	if changed := reader.Changed(); len(changed) > 0 {
		r := Result{CheckID: "input-stability", Source: "core", Required: true, Status: "unavailable", Diagnostics: []Diagnostic{}, Evidence: []string{}, Coverage: "Observed file content, path resolution and link existence"}
		for _, file := range changed {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{RuleID: "input.changed", Message: "Observed input changed during execution", Location: file})
		}
		results = append(results, r)
	}
	code := 0
	warnings := []string{}
	for _, r := range results {
		if !r.Required {
			if r.Status != "passed" {
				warnings = append(warnings, r.CheckID)
			}
			continue
		}
		if r.Status == "unavailable" || r.Status == "not-run" {
			code = 2
		} else if r.Status == "failed" && code == 0 {
			code = 1
		}
	}
	status := []string{"passed", "failed", "unavailable"}[code]
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, 2, err
	}
	identityData, _ := json.Marshal(struct {
		Request Request
		Files   map[string]string
	}{req, reader.Files})
	return map[string]any{"protocolVersion": ProtocolVersion, "taskId": req.TaskID, "round": req.Round, "identity": map[string]any{"runId": hex.EncodeToString(id), "coreVersion": Version, "rulesDigest": digest([]byte(checks.SchemaJSON)), "inputDigest": digest(identityData), "files": reader.Files}, "checks": results, "summary": Summary{Status: status, ExitCode: code, Warnings: warnings}}, code, nil
}
