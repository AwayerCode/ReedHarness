package systemdesign

import (
	"fmt"
	"slices"
)

// recordDependency applies declared module rules to an already resolved source edge.
func recordDependency(owner, to *Module, rel string, line int, observed map[string][]string, r *Report) {
	r.Edges = append(r.Edges, Edge{owner.ID, to.ID, rel, line})
	if !slices.Contains(observed[owner.ID], to.ID) {
		observed[owner.ID] = append(observed[owner.ID], to.ID)
	}
	if !slices.Contains(owner.Allowed, to.ID) {
		r.Add("dependency.forbidden", fmt.Sprintf("%s:%d", rel, line), owner.ID+" -> "+to.ID+" is not allowed")
	}
}
