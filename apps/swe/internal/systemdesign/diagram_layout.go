package systemdesign

// diagramLayout is the geometry shared by node placement and edge routing.
type diagramLayout struct {
	View      diagramView
	Nodes     map[string]viewModule
	Layers    map[string]viewLayer
	RowOf     map[string]int
	SameCount []int
	Columns   int
}

func layoutDiagram(d *Design, r Report) diagramLayout {
	v := diagramView{Report: r}
	rows := orderDiagramModules(d)
	layerEdges, columns := 0, 1
	rowOf := map[string]int{}
	for i, l := range d.Layers {
		layerEdges += len(l.Allowed)
		columns = max(columns, len(rows[i]))
		for _, m := range rows[i] {
			rowOf[m.ID] = i
		}
	}
	sameCount := make([]int, len(rows))
	for _, m := range d.Modules {
		for _, dep := range m.Allowed {
			if rowOf[m.ID] == rowOf[dep] {
				sameCount[rowOf[m.ID]]++
			}
		}
	}
	v.BandX = 44 + layerEdges*14
	v.BandWidth = 112 + columns*280
	v.Width = v.BandX + v.BandWidth + 24
	nodes := map[string]viewModule{}
	layerNodes := map[string]viewLayer{}
	y := 24
	for i, l := range d.Layers {
		vl := viewLayer{Layer: l, Y: y, LabelX: v.BandX + 24, Color: i % 4}
		vl.Lines = diagramLines(l.ID+" · "+l.Responsibility, (v.BandWidth-48)/9)
		headerHeight := 28 + len(vl.Lines)*23
		height := 90
		for _, m := range rows[i] {
			lines := append(diagramLines(m.ID, 24), diagramLines(m.Responsibility, 26)...)
			height = max(height, 36+len(lines)*23)
			vl.Modules = append(vl.Modules, viewModule{Module: m, Path: d.ModulePath(m), Lines: lines})
		}
		vl.Height = headerHeight + height + 24 + sameCount[i]*14
		vl.LabelY = y
		for j := range vl.Modules {
			m := &vl.Modules[j]
			m.X, m.Y, m.Width, m.Height = v.BandX+56+j*280, y+headerHeight, 224, height
			nodes[m.ID] = *m
		}
		v.Layers = append(v.Layers, vl)
		layerNodes[l.ID] = vl
		y += vl.Height + 110
	}
	v.Height = y - 86
	return diagramLayout{v, nodes, layerNodes, rowOf, sameCount, columns}
}
