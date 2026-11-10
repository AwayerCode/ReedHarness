package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/AwayerCode/ReedHarness/apps/reed/internal/checks"
	"github.com/AwayerCode/ReedHarness/apps/reed/internal/workspace"
	"path/filepath"
)

func absoluteInput(root, value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, value)
}
func related(id string, raw json.RawMessage, root string, schemas map[string]checks.Schema, reader *workspace.Observer) (checks.Report, error) {
	r := checks.NewReport()
	switch id {
	case "design":
		var input struct {
			SystemDesign string `json:"systemDesign"`
			Protocol     string `json:"protocol"`
		}
		if e := strict(raw, &input); e != nil {
			return r, e
		}
		if input.SystemDesign == "" || input.Protocol == "" {
			return r, errors.New("design requires systemDesign and protocol")
		}
		return checks.Design(absoluteInput(root, input.SystemDesign), absoluteInput(root, input.Protocol), schemas, reader)
	case "artifacts":
		var input struct {
			Kind     string `json:"kind"`
			File     string `json:"file"`
			Protocol string `json:"protocol"`
		}
		if e := strict(raw, &input); e != nil {
			return r, e
		}
		if input.File == "" {
			return r, errors.New("artifacts requires file")
		}
		return checks.Artifacts(input.Kind, absoluteInput(root, input.File), absoluteInput(root, input.Protocol), schemas, reader)
	case "structure":
		var input struct {
			Design string   `json:"design"`
			Scopes []string `json:"scopes"`
		}
		if e := strict(raw, &input); e != nil {
			return r, e
		}
		if input.Design == "" {
			return r, errors.New("structure requires design")
		}
		return checks.Structure(root, absoluteInput(root, input.Design), input.Scopes, schemas, reader)
	default:
		return r, fmt.Errorf("check not implemented: %s", id)
	}
}
