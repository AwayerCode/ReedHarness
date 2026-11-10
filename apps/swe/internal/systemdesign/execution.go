package systemdesign

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// Execute evaluates one design without CLI parsing or output-channel handling.
func Execute(s Scope, mode, profile, version string) (Report, []byte, int) {
	return NewSession().Execute(s, mode, profile, version)
}

func (session *Session) Execute(s Scope, mode, profile, version string) (Report, []byte, int) {
	data, err := readScopeInput(s)
	if err != nil {
		r := UnavailableReport(mode, version, err)
		r.Scope = &s
		return r, nil, 2
	}
	return session.executeData(s, data, mode, profile, version)
}

func readScopeInput(s Scope) ([]byte, error) {
	if s.Unavailable != "" {
		return nil, errors.New(s.Unavailable)
	}
	info, err := os.Lstat(s.File)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("design must be a regular file")
	}
	input, err := os.Open(s.File)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	return io.ReadAll(io.LimitReader(input, 1024*1024+1))
}

func (session *Session) executeData(s Scope, data []byte, mode, profile, version string) (Report, []byte, int) {
	r := NewReport(mode, version, data)
	r.Scope = &s
	d := ParseProfile(data, &r, profile == "layers")
	if profile != "" {
		r.Unverified = append(r.Unverified, "Partial layers profile: not a complete implementation acceptance.")
	}
	if len(r.Diagnostics) == 0 {
		d.ValidateScope(s, &r)
	}
	if len(r.Diagnostics) == 0 && mode == "check" {
		if err := d.checkImplementation(s.Root, &r, session); err != nil {
			r.Status = "unavailable"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Rule: "implementation.unavailable", Message: err.Error()})
			return r, nil, 2
		}
	}
	if len(r.Diagnostics) > 0 {
		return r, nil, 1
	}
	if mode == "render" {
		var html bytes.Buffer
		if err := Render(&html, d, r); err != nil {
			r.Status = "unavailable"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Rule: "render.unavailable", Message: err.Error()})
			return r, nil, 2
		}
		return r, html.Bytes(), 0
	}
	return r, nil, 0
}

func UnavailableReport(mode, version string, err error) Report {
	r := NewReport(mode, version, nil)
	r.Digest = ""
	r.Status = "unavailable"
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Rule: "system-design.unavailable", Message: err.Error()})
	return r
}
