package systemdesign

import (
	"math"
	"sort"
)

type diagramPosition struct {
	row, col int
}

type diagramLink struct {
	from, to string
}

type diagramRowPair struct {
	upper, lower int
}

type diagramLinkEndpoints struct {
	upper, lower int
}

// diagramOrderState keeps node positions and edge adjacency stable so an
// adjacent swap can update the exact score without rebuilding every edge pair.
type diagramOrderState struct {
	positions map[string]diagramPosition
	links     []diagramLink
	incident  map[string][]int
}

func newDiagramOrderState(rows [][]Module, modules []Module) *diagramOrderState {
	state := &diagramOrderState{
		positions: map[string]diagramPosition{},
		incident:  map[string][]int{},
	}
	for row, modules := range rows {
		for col, module := range modules {
			state.positions[module.ID] = diagramPosition{row: row, col: col}
		}
	}
	for _, module := range modules {
		for _, dependency := range module.Allowed {
			index := len(state.links)
			state.links = append(state.links, diagramLink{from: module.ID, to: dependency})
			state.incident[module.ID] = append(state.incident[module.ID], index)
			state.incident[dependency] = append(state.incident[dependency], index)
		}
	}
	return state
}

func edgeDistance(from, to diagramPosition) float64 {
	if from.row == to.row {
		return math.Abs(float64(from.col-to.col)) * 0.4
	}
	return math.Abs(float64(from.col-to.col)) * 0.15
}

func edgeCrosses(left, right diagramLink, positions map[string]diagramPosition) bool {
	a, b := positions[left.from], positions[left.to]
	c, d := positions[right.from], positions[right.to]
	if a.row > b.row {
		a, b = b, a
	}
	if c.row > d.row {
		c, d = d, c
	}
	return a.row == c.row && b.row == d.row && (a.col-c.col)*(b.col-d.col) < 0
}

type diagramFenwick []int

func (tree diagramFenwick) add(index int) {
	for index++; index < len(tree); index += index & -index {
		tree[index]++
	}
}

func (tree diagramFenwick) prefix(index int) int {
	total := 0
	for index++; index > 0; index -= index & -index {
		total += tree[index]
	}
	return total
}

// crossingCount counts strict inversions between two rows in O(n log n).
func crossingCount(links []diagramLinkEndpoints) int {
	if len(links) < 2 {
		return 0
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].upper != links[j].upper {
			return links[i].upper < links[j].upper
		}
		return links[i].lower < links[j].lower
	})
	columns := 1
	for _, link := range links {
		columns = max(columns, link.lower+1)
	}
	tree := make(diagramFenwick, columns+1)
	crossings, seen := 0, 0
	for _, link := range links {
		crossings += seen - tree.prefix(link.lower)
		tree.add(link.lower)
		seen++
	}
	return crossings
}

func (state *diagramOrderState) fullScore() float64 {
	score := 0.0
	groups := map[diagramRowPair][]diagramLinkEndpoints{}
	for _, link := range state.links {
		from, to := state.positions[link.from], state.positions[link.to]
		score += edgeDistance(from, to)
		if from.row == to.row {
			continue
		}
		if from.row > to.row {
			from, to = to, from
		}
		key := diagramRowPair{upper: from.row, lower: to.row}
		groups[key] = append(groups[key], diagramLinkEndpoints{upper: from.col, lower: to.col})
	}
	for _, links := range groups {
		score += float64(crossingCount(links) * 10)
	}
	return score
}

// only incident edges can change after swapping two adjacent nodes. Their
// distances and mutual crossings therefore form the complete score delta.
func (state *diagramOrderState) localScore(first, second string) float64 {
	seen := map[int]bool{}
	indexes := make([]int, 0, len(state.incident[first])+len(state.incident[second]))
	for _, name := range []string{first, second} {
		for _, index := range state.incident[name] {
			if seen[index] {
				continue
			}
			seen[index] = true
			indexes = append(indexes, index)
		}
	}
	score := 0.0
	for _, index := range indexes {
		link := state.links[index]
		score += edgeDistance(state.positions[link.from], state.positions[link.to])
	}
	for i, left := range indexes {
		for _, right := range indexes[i+1:] {
			if edgeCrosses(state.links[left], state.links[right], state.positions) {
				score += 10
			}
		}
	}
	return score
}

func (state *diagramOrderState) swap(first, second string) {
	state.positions[first], state.positions[second] = state.positions[second], state.positions[first]
}

// Order modules to reduce crossings between layers while retaining layer order.
func orderDiagramModules(d *Design) [][]Module {
	rows := make([][]Module, len(d.Layers))
	for i, layer := range d.Layers {
		for _, module := range d.Modules {
			if module.LayerID == layer.ID {
				rows[i] = append(rows[i], module)
			}
		}
	}
	state := newDiagramOrderState(rows, d.Modules)
	for pass := 0; pass < 12; pass++ {
		changed := false
		for row := range rows {
			for col := 0; col+1 < len(rows[row]); col++ {
				first, second := rows[row][col], rows[row][col+1]
				before := state.localScore(first.ID, second.ID)
				state.swap(first.ID, second.ID)
				after := state.localScore(first.ID, second.ID)
				if after < before-0.001 {
					rows[row][col], rows[row][col+1] = second, first
					changed = true
				} else {
					state.swap(first.ID, second.ID)
				}
			}
		}
		if !changed {
			break
		}
	}
	return rows
}
