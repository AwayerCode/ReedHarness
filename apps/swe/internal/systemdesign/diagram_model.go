package systemdesign

import (
	"strings"
)

type viewModule struct {
	Module
	Path                string
	X, Y, Width, Height int
	Lines               []string
}
type viewLayer struct {
	Layer
	Modules                   []viewModule
	Y, Height, LabelX, LabelY int
	Lines                     []string
	Color                     int
}
type viewEdge struct {
	From, To, Path string
	Kind           string
}
type diagramView struct {
	Layers                          []viewLayer
	Edges                           []viewEdge
	Report                          Report
	Width, Height, BandX, BandWidth int
}

// Wrap by display cells so Chinese descriptions and long identifiers remain inside nodes.
func diagramLines(s string, limit int) []string {
	var lines []string
	var line strings.Builder
	width := 0
	for _, c := range s {
		w := 1
		if c > 127 {
			w = 2
		}
		if c == '\n' || width+w > limit {
			lines = append(lines, line.String())
			line.Reset()
			width = 0
		}
		if c != '\n' {
			line.WriteRune(c)
			width += w
		}
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}
	return lines
}
