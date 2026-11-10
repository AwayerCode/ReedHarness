package systemdesign

import (
	"fmt"
	"math"
	"sort"
)

func routeDiagram(d *Design, layout diagramLayout) []viewEdge {
	v := layout.View
	nodes, layerNodes, rowOf := layout.Nodes, layout.Layers, layout.RowOf
	sameCount, columns := layout.SameCount, layout.Columns
	lane := 0

	for _, l := range d.Layers {
		for _, to := range l.Allowed {
			a, b := layerNodes[l.ID], layerNodes[to]
			x := v.BandX - 20 - lane*14
			sy, ty := a.Y+60, b.Y+60
			v.Edges = append(v.Edges, viewEdge{l.ID, to, fmt.Sprintf("M %d %d H %d V %d H %d", v.BandX, sy, x, ty, v.BandX), "layer"})
			lane++
		}
	}
	// Distinct ports ordered by the other endpoint avoid shared arrowheads and stems.
	incoming, outgoing := map[string][]string{}, map[string][]string{}
	for _, m := range d.Modules {
		for _, dep := range m.Allowed {
			outgoing[m.ID] = append(outgoing[m.ID], dep)
			incoming[dep] = append(incoming[dep], m.ID)
		}
	}
	for _, ports := range []map[string][]string{incoming, outgoing} {
		for _, ids := range ports {
			sort.SliceStable(ids, func(i, j int) bool { return nodes[ids[i]].X < nodes[ids[j]].X })
		}
	}
	port := func(m viewModule, other string, ids []string) int {
		for i, id := range ids {
			if id == other {
				return m.X + 20 + (m.Width-40)*(i+1)/(len(ids)+1)
			}
		}
		return m.X + m.Width/2
	}
	sameLane := make([]int, len(d.Layers))
	gapUse := make([]int, columns+1)
	for _, m := range d.Modules {
		for _, to := range m.Allowed {
			a, b := nodes[m.ID], nodes[to]
			ar, br := rowOf[m.ID], rowOf[to]
			sx, tx := port(a, to, outgoing[m.ID]), port(b, m.ID, incoming[to])
			outIndex, inIndex := 0, 0
			for i, id := range outgoing[m.ID] {
				if id == to {
					outIndex = i
				}
			}
			for i, id := range incoming[to] {
				if id == m.ID {
					inIndex = i
				}
			}
			// Reserve the left bottom ports for same-row arrivals, the right for departures.
			sx = a.X + 84 + (a.Width-104)*(outIndex+1)/(len(outgoing[m.ID])+1)
			sy, ty := a.Y+a.Height, b.Y
			path := ""
			if ar == br {
				// Same-layer dependencies occupy only this layer's lower routing strip.
				ty = b.Y + b.Height
				tx = b.X + 12 + 60*(inIndex+1)/(len(incoming[to])+1)
				lower := sy + 18 + sameLane[ar]*14
				path = fmt.Sprintf("M %d %d V %d H %d V %d", sx, sy, lower, tx, ty)
				sameLane[ar]++
			} else {
				direction := 1
				if ar > br {
					direction = -1
					sy = a.Y
					ty = b.Y + b.Height
				}
				if ar-br == 1 || br-ar == 1 {
					// Adjacent rows use orthogonal paths, matching the architecture overview.
					middle := (sy + ty) / 2
					path = fmt.Sprintf("M %d %d V %d H %d V %d", sx, sy, middle, tx, ty)
				} else {
					// All rows share module columns. Their gutters are obstacle-free, even across skipped rows.
					chosen, best := 0, math.MaxFloat64
					for k := 0; k <= columns; k++ {
						x := v.BandX + 28 + k*280
						score := math.Abs(float64(sx-x)) + math.Abs(float64(tx-x)) + float64(gapUse[k]*60)
						if score < best {
							best = score
							chosen = k
						}
					}
					x := v.BandX + 28 + chosen*280 + (gapUse[chosen]%5-2)*6
					gapUse[chosen]++
					startY := sy + direction*(46+min(outIndex, 10)*6)
					if direction > 0 {
						startY += sameCount[ar] * 14
					}
					endY := ty - direction*(30+min(inIndex, 10)*6)
					path = fmt.Sprintf("M %d %d V %d H %d V %d H %d V %d", sx, sy, startY, x, endY, tx, ty)
				}
			}
			v.Edges = append(v.Edges, viewEdge{m.ID, to, path, "module"})
		}
	}
	return v.Edges
}
