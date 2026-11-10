package systemdesign

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDiagramContainsLayersModulesAndDirectedEdges(t *testing.T) {
	_, data := fixture(t)
	report := NewReport("render", "test", data)
	design := Parse(data, &report)
	var out bytes.Buffer
	if err := Render(&out, design, report); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		`<svg`, `id="layer-application"`, `id="layer-infrastructure"`,
		`id="module-orders"`, `id="module-payments"`, `id="module-storage"`,
		`class="edge edge-layer" data-from="application" data-to="infrastructure"`,
		`class="edge edge-module" data-from="orders" data-to="payments"`,
		`class="edge edge-module" data-from="payments" data-to="storage"`,
		`marker-end:url(#arrow-module)`, `href="#info-orders"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(html, `class="edge edge-`) != 3 {
		t.Fatal("unexpected dependency count")
	}
	// Every module must be contained by its declared layer's band, including long labels.
	design.Modules[0].Responsibility = strings.Repeat("长职责描述", 30)
	view := makeDiagram(design, report)
	for _, layer := range view.Layers {
		for _, module := range layer.Modules {
			if module.Y < layer.LabelY+28+(len(layer.Lines)-1)*23+16 {
				t.Fatal("module overlaps layer heading")
			}
			if module.Y < layer.Y || module.Y+module.Height > layer.Y+layer.Height {
				t.Fatal("module outside layer")
			}
			if module.X < view.BandX || module.X+module.Width > view.BandX+view.BandWidth {
				t.Fatal("module outside band")
			}
			if 28+(len(module.Lines)-1)*23 > module.Height {
				t.Fatal("text outside module")
			}
		}
	}
}

func TestDenseDiagramUsesCompactLocalRoutes(t *testing.T) {
	d := &Design{Source: "src"}
	for _, name := range []string{"a", "b", "c", "d"} {
		d.Layers = append(d.Layers, Layer{ID: name, Directory: name})
		for _, suffix := range []string{"one", "two", "three", "four"} {
			d.Modules = append(d.Modules, Module{Layer: Layer{ID: name + "-" + suffix, Directory: suffix}, LayerID: name})
		}
	}
	for i := range d.Modules {
		if i%4 < 3 {
			d.Modules[i].Allowed = append(d.Modules[i].Allowed, d.Modules[i+1].ID)
		}
		if i+4 < len(d.Modules) {
			d.Modules[i].Allowed = append(d.Modules[i].Allowed, d.Modules[i+4].ID)
		}
	}
	d.Modules[0].Allowed = append(d.Modules[0].Allowed, d.Modules[15].ID)
	v := makeDiagram(d, Report{})
	if v.Height > 1300 {
		t.Fatalf("excessive routing whitespace: %d", v.Height)
	}
	adjacent := 0
	for _, e := range v.Edges {
		if strings.Contains(e.Path, " C ") {
			t.Fatalf("non-orthogonal route: %s", e.Path)
		}
		if e.To[0]-e.From[0] == 1 {
			var sx, sy, mid, tx, ty int
			if n, err := fmt.Sscanf(e.Path, "M %d %d V %d H %d V %d", &sx, &sy, &mid, &tx, &ty); err != nil || n != 5 {
				t.Fatalf("invalid adjacent route: %s", e.Path)
			}
			if mid <= sy || mid >= ty {
				t.Fatalf("bend outside row gap: %s", e.Path)
			}
			adjacent++
		}
	}
	if adjacent != 12 {
		t.Fatalf("want 12 local adjacent routes, got %d", adjacent)
	}
	// A skip-layer route must pass through column gutters, never intermediate module interiors.
	var skip viewEdge
	for _, e := range v.Edges {
		if e.From == "a-one" && e.To == "d-four" {
			skip = e
		}
	}
	var sx, sy, y1, x, y2, tx, ty int
	n, err := fmt.Sscanf(skip.Path, "M %d %d V %d H %d V %d H %d V %d", &sx, &sy, &y1, &x, &y2, &tx, &ty)
	if err != nil || n != 7 {
		t.Fatalf("invalid skip route %q", skip.Path)
	}
	for _, l := range v.Layers[1:3] {
		for _, m := range l.Modules {
			if x > m.X && x < m.X+m.Width {
				t.Fatalf("skip route crosses %s", m.ID)
			}
		}
	}
	// Repeated rendering must not depend on map iteration order.
	var a, b bytes.Buffer
	if err := Render(&a, d, Report{}); err != nil {
		t.Fatal(err)
	}
	if err := Render(&b, d, Report{}); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("unstable diagram")
	}
}

func TestSkipRoutesStayBetweenDenseRows(t *testing.T) {
	d := &Design{Source: "src"}
	for _, layer := range []string{"a", "b", "c"} {
		d.Layers = append(d.Layers, Layer{ID: layer})
		for i := 0; i < 20; i++ {
			m := Module{Layer: Layer{ID: fmt.Sprintf("%s-%d", layer, i)}, LayerID: layer}
			if layer == "a" {
				m.Allowed = []string{"c-0"}
			}
			d.Modules = append(d.Modules, m)
		}
	}
	v := makeDiagram(d, Report{})
	for _, e := range v.Edges {
		var sx, sy, y1, x, y2, tx, ty int
		if _, err := fmt.Sscanf(e.Path, "M %d %d V %d H %d V %d H %d V %d", &sx, &sy, &y1, &x, &y2, &tx, &ty); err != nil {
			t.Fatal(err)
		}
		for _, m := range v.Layers[1].Modules {
			if y1 >= m.Y || y2 <= m.Y+m.Height {
				t.Fatalf("skip route crosses middle-row height: %s", e.Path)
			}
		}
	}
}

func TestModuleInterfacesAreValidatedAndEscaped(t *testing.T) {
	_, data := fixture(t)
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{`[{"name":"Reserve(ctx, sku, quantity)","description":"预留库存 <script>"}]`, true},
		{`[{"name":"Reserve"}]`, false},
		{`[{"name":" ","description":"预留库存"}]`, false},
	} {
		r := NewReport("render", "test", data)
		d := Parse(data, &r)
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		if err = json.Unmarshal(raw, &obj); err != nil {
			t.Fatal(err)
		}
		var value any
		if err = json.Unmarshal([]byte(tc.value), &value); err != nil {
			t.Fatal(err)
		}
		obj["modules"].([]any)[0].(map[string]any)["interfaces"] = value
		raw, err = json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		r = NewReport("render", "test", raw)
		d = Parse(raw, &r)
		if (r.Status == "passed") != tc.valid {
			t.Fatalf("unexpected validation result: %+v", r)
		}
		if tc.valid {
			var out bytes.Buffer
			if err = Render(&out, d, r); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Reserve(ctx, sku, quantity)") || !strings.Contains(out.String(), "&lt;script&gt;") {
				t.Fatal("interface omitted or unescaped")
			}
		}
	}
}
