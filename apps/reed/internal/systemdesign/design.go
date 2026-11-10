// Package systemdesign validates directory-backed module systemdesign.
package systemdesign

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

type Layer struct {
	ID             string   `json:"id"`
	Directory      string   `json:"directory"`
	Responsibility string   `json:"responsibility"`
	Allowed        []string `json:"allowed_dependencies"`
}
type ModuleInterface struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Module struct {
	Interfaces []ModuleInterface `json:"interfaces,omitempty"`
	Layer
	LayerID  string   `json:"layer"`
	Required []string `json:"required_files"`
}
type Design struct {
	Version int      `json:"schema_version"`
	Source  string   `json:"source_root"`
	Layers  []Layer  `json:"layers"`
	Modules []Module `json:"modules"`
}
type Diagnostic struct {
	Rule     string `json:"rule"`
	Location string `json:"location"`
	Message  string `json:"message"`
}
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	File string `json:"file"`
	Line int    `json:"line"`
}
type Report struct {
	Scope       *Scope       `json:"scope,omitempty"`
	Format      string       `json:"format"`
	Version     string       `json:"generator_version"`
	Digest      string       `json:"design_sha256"`
	Mode        string       `json:"mode"`
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Edges       []Edge       `json:"observed_dependencies"`
	Coverage    []string     `json:"coverage"`
	Unverified  []string     `json:"unverified"`
}

func NewReport(mode, version string, data []byte) Report {
	h := sha256.Sum256(data)
	return Report{Format: "swe-system-design-v1", Version: version, Digest: hex.EncodeToString(h[:]), Mode: mode, Status: "passed", Diagnostics: []Diagnostic{}, Edges: []Edge{}, Coverage: []string{}, Unverified: []string{"Interfaces, runtime calls and behavior are not checked."}}
}
func (r *Report) Add(rule, loc, message string) {
	r.Diagnostics = append(r.Diagnostics, Diagnostic{rule, loc, message})
	r.Status = "violations"
}
func SafePath(p string) bool {
	return p != "" && p != "." && path.Clean(p) == p && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "../") && !strings.ContainsAny(p, "\\:\x00") && p != ".."
}

// YAML is restricted to JSON-compatible nodes: no aliases, merge keys, custom tags or duplicate keys.
func nodeValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 1 {
			return nodeValue(n.Content[0])
		}
	case yaml.MappingNode:
		if n.Tag != "!!map" {
			break
		}
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return nil, fmt.Errorf("line %d: mapping keys must be strings", k.Line)
			}
			if _, ok := m[k.Value]; ok {
				return nil, fmt.Errorf("line %d: duplicate key %s", k.Line, k.Value)
			}
			v, e := nodeValue(n.Content[i+1])
			if e != nil {
				return nil, e
			}
			m[k.Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		if n.Tag != "!!seq" {
			break
		}
		a := []any{}
		for _, v := range n.Content {
			x, e := nodeValue(v)
			if e != nil {
				return nil, e
			}
			a = append(a, x)
		}
		return a, nil
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!str":
			return n.Value, nil
		case "!!int", "!!float", "!!bool", "!!null":
			var v any
			if e := n.Decode(&v); e != nil {
				return nil, e
			}
			b, e := json.Marshal(v)
			if e != nil {
				return nil, e
			}
			e = json.Unmarshal(b, &v)
			return v, e
		}
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node or tag", n.Line)
}
func Parse(data []byte, r *Report) *Design { return ParseProfile(data, r, false) }

func ParseProfile(data []byte, r *Report, partial bool) *Design {
	if len(data) > 1024*1024 {
		r.Add("yaml.size", "", "design exceeds 1 MiB")
		return nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var n, extra yaml.Node
	if e := dec.Decode(&n); e != nil {
		r.Add("yaml.parse", "", e.Error())
		return nil
	}
	if e := dec.Decode(&extra); e != io.EOF {
		r.Add("yaml.documents", "", "exactly one YAML document required")
		return nil
	}
	v, e := nodeValue(&n)
	if e != nil {
		r.Add("yaml.parse", "", e.Error())
		return nil
	}
	c := jsonschema.NewCompiler()
	if e = c.AddResource("systemdesign.json", strings.NewReader(SchemaJSON)); e != nil {
		panic(e)
	}
	schema, e := c.Compile("systemdesign.json")
	if e != nil {
		panic(e)
	}
	if e = schema.Validate(v); e != nil {
		r.Add("schema", "", e.Error())
		return nil
	}
	b, _ := json.Marshal(v)
	var d Design
	if e = json.Unmarshal(b, &d); e != nil {
		r.Add("schema", "", e.Error())
		return nil
	}
	if !partial && len(d.Modules) == 0 {
		r.Add("design.incomplete", "modules", "complete design requires at least one module")
	}
	d.Validate(r)
	r.Coverage = append(r.Coverage, "JSON Schema, references, unique directory ownership, declared dependency cycles")
	return &d
}
func (d *Design) ModulePath(m Module) string {
	for _, l := range d.Layers {
		if l.ID == m.LayerID {
			return path.Join(d.Source, l.Directory, m.Directory)
		}
	}
	return ""
}
func (d *Design) Validate(r *Report) {
	if !SafePath(d.Source) {
		r.Add("path.source", d.Source, "source_root must be a clean relative directory")
	}
	layers := map[string]Layer{}
	dirs := map[string]bool{}
	modules := map[string]Module{}
	paths := map[string]bool{}
	for _, l := range d.Layers {
		if _, ok := layers[l.ID]; ok {
			r.Add("id.duplicate", l.ID, "duplicate layer")
		}
		layers[l.ID] = l
		k := strings.ToLower(l.Directory)
		if dirs[k] {
			r.Add("path.overlap", l.Directory, "layer directory collision")
		}
		dirs[k] = true
	}
	for _, m := range d.Modules {
		if _, ok := modules[m.ID]; ok {
			r.Add("id.duplicate", m.ID, "duplicate module")
		}
		modules[m.ID] = m
		if _, ok := layers[m.LayerID]; !ok {
			r.Add("reference.layer", m.ID, "undefined layer "+m.LayerID)
		}
		p := d.ModulePath(m)
		k := strings.ToLower(p)
		if paths[k] {
			r.Add("path.overlap", p, "module directory collision")
		}
		paths[k] = true
		files := map[string]string{}
		for _, f := range m.Required {
			if !SafePath(f) {
				r.Add("path.file", m.ID, "invalid required file "+f)
				continue
			}
			key := strings.ToLower(f)
			if previous, ok := files[key]; ok {
				r.Add("path.file-conflict", m.ID, "required files collide: "+previous+" and "+f)
			}
			files[key] = f
		}
		// Build the complete set first so detection does not depend on declaration order.
		for _, f := range m.Required {
			if !SafePath(f) {
				continue
			}
			for parent := path.Dir(strings.ToLower(f)); parent != "."; parent = path.Dir(parent) {
				if ancestor, ok := files[parent]; ok {
					r.Add("path.file-conflict", m.ID, "required file must also be a directory: "+ancestor+" contains "+f)
				}
			}
		}
	}
	lg := map[string][]string{}
	mg := map[string][]string{}
	for _, l := range d.Layers {
		lg[l.ID] = l.Allowed
		for _, dep := range l.Allowed {
			if _, ok := layers[dep]; !ok {
				r.Add("reference.layer", l.ID, "undefined dependency "+dep)
			}
			if dep == l.ID {
				r.Add("dependency.self", l.ID, "self layer dependency")
			}
		}
	}
	for _, m := range d.Modules {
		mg[m.ID] = m.Allowed
		for _, dep := range m.Allowed {
			target, ok := modules[dep]
			if !ok {
				r.Add("reference.module", m.ID, "undefined dependency "+dep)
				continue
			}
			if target.LayerID != m.LayerID && !slices.Contains(layers[m.LayerID].Allowed, target.LayerID) {
				r.Add("dependency.layer", m.ID, "layer forbids dependency on "+dep)
			}
		}
	}
	cycles(lg, "layer", r)
	cycles(mg, "module", r)
}
func cycles(g map[string][]string, kind string, r *Report) {
	state := map[string]int{}
	var visit func(string, []string)
	visit = func(n string, stack []string) {
		if state[n] == 1 {
			r.Add("dependency.cycle", n, kind+" cycle: "+strings.Join(append(stack, n), " -> "))
			return
		}
		if state[n] == 2 {
			return
		}
		state[n] = 1
		for _, v := range g[n] {
			visit(v, append(stack, n))
		}
		state[n] = 2
	}
	keys := []string{}
	for k := range g {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		visit(k, nil)
	}
}
