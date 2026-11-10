package systemdesign

import (
	"fmt"
	"io"
	"math"
	"testing"
	"time"
)

func denseOrderingDesign(layers, modulesPerLayer, dependencies int) *Design {
	design := &Design{Version: 1, Source: "src"}
	for layer := 0; layer < layers; layer++ {
		id := fmt.Sprintf("l%d", layer)
		allowedLayers := []string{}
		if layer+1 < layers {
			allowedLayers = append(allowedLayers, fmt.Sprintf("l%d", layer+1))
		}
		design.Layers = append(design.Layers, Layer{
			ID: id, Directory: id, Responsibility: "layer", Allowed: allowedLayers,
		})
		for module := 0; module < modulesPerLayer; module++ {
			design.Modules = append(design.Modules, Module{
				Layer: Layer{
					ID:             fmt.Sprintf("l%d-m%d", layer, module),
					Directory:      fmt.Sprintf("m%d", module),
					Responsibility: "module",
					Allowed:        []string{},
				},
				LayerID:  id,
				Required: []string{},
			})
		}
	}
	for index := range design.Modules {
		module := &design.Modules[index]
		layer := index / modulesPerLayer
		if layer+1 >= layers {
			continue
		}
		for dependency := 0; dependency < dependencies; dependency++ {
			module.Allowed = append(module.Allowed, fmt.Sprintf("l%d-m%d", layer+1, dependency))
		}
	}
	return design
}

// legacyOrderDiagramModules preserves the previous reference behavior for
// small deterministic inputs while the optimized implementation is regression
// tested against it.
func legacyOrderDiagramModules(design *Design) [][]Module {
	rows := make([][]Module, len(design.Layers))
	for i, layer := range design.Layers {
		for _, module := range design.Modules {
			if module.LayerID == layer.ID {
				rows[i] = append(rows[i], module)
			}
		}
	}
	cost := func() float64 {
		type pos struct{ row, col int }
		positions := map[string]pos{}
		for i, row := range rows {
			for j, module := range row {
				positions[module.ID] = pos{i, j}
			}
		}
		type link struct{ from, to pos }
		links := []link{}
		score := 0.0
		for _, module := range design.Modules {
			for _, dependency := range module.Allowed {
				from, to := positions[module.ID], positions[dependency]
				if from.row == to.row {
					score += math.Abs(float64(from.col-to.col)) * 0.4
					continue
				}
				score += math.Abs(float64(from.col-to.col)) * 0.15
				if from.row > to.row {
					from, to = to, from
				}
				links = append(links, link{from, to})
			}
		}
		for i, left := range links {
			for _, right := range links[i+1:] {
				if left.from.row == right.from.row &&
					left.to.row == right.to.row &&
					(left.from.col-right.from.col)*(left.to.col-right.to.col) < 0 {
					score += 10
				}
			}
		}
		return score
	}
	best := cost()
	for pass := 0; pass < 12; pass++ {
		changed := false
		for row := range rows {
			for col := 0; col+1 < len(rows[row]); col++ {
				rows[row][col], rows[row][col+1] = rows[row][col+1], rows[row][col]
				candidate := cost()
				if candidate < best-0.001 {
					best = candidate
					changed = true
				} else {
					rows[row][col], rows[row][col+1] = rows[row][col+1], rows[row][col]
				}
			}
		}
		if !changed {
			break
		}
	}
	return rows
}

func flattenOrder(rows [][]Module) []string {
	order := []string{}
	for _, row := range rows {
		for _, module := range row {
			order = append(order, module.ID)
		}
	}
	return order
}

func TestOptimizedOrderingMatchesLegacy(t *testing.T) {
	designs := []*Design{denseOrderingDesign(4, 10, 6), denseOrderingDesign(6, 12, 9)}
	zigzag := denseOrderingDesign(5, 9, 4)
	for index := range zigzag.Modules {
		zigzag.Modules[index].Allowed = append(zigzag.Modules[index].Allowed, fmt.Sprintf("l%d-m%d", index%5, (index*7)%9))
	}
	designs = append(designs, zigzag)
	for caseIndex, design := range designs {
		got := flattenOrder(orderDiagramModules(design))
		want := flattenOrder(legacyOrderDiagramModules(design))
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("case %d differs:\n got %v\nwant %v", caseIndex, got, want)
		}
	}
}

func TestDenseDiagramRenderPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("dense render regression")
	}
	design := denseOrderingDesign(8, 50, 15)
	const budget = 8 * time.Second
	started := time.Now()
	if err := Render(io.Discard, design, Report{}); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if elapsed > budget {
		t.Fatalf("dense render took %s; budget %s", elapsed, budget)
	}
}

func BenchmarkDenseDiagramOrdering(b *testing.B) {
	design := denseOrderingDesign(8, 50, 15)
	b.ResetTimer()
	for range b.N {
		orderDiagramModules(design)
	}
}
