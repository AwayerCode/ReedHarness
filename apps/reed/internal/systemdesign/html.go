package systemdesign

import (
	_ "embed"
	"html/template"
	"io"
)

// makeDiagram assembles the ordered layout and its routed dependencies.
func makeDiagram(d *Design, r Report) diagramView {
	layout := layoutDiagram(d, r)
	layout.View.Edges = routeDiagram(d, layout)
	return layout.View
}

func Render(w io.Writer, d *Design, r Report) error {
	return page.Execute(w, makeDiagram(d, r))
}

//go:embed architecture.html.tmpl
var pageSource string
var page = template.Must(template.New("architecture").Funcs(template.FuncMap{
	"add":   func(a, b int) int { return a + b },
	"lineY": func(y, i int) int { return y + 28 + i*23 },
}).Parse(pageSource))
