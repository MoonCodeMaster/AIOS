package spec

import (
	"errors"
	"fmt"
	"sort"
)

// TopologicalOrder returns tasks ordered so every dep appears before its dependents.
// Ties (same indegree group) are broken alphabetically by ID for determinism.
func TopologicalOrder(tasks []*Task) ([]*Task, error) {
	byID := map[string]*Task{}
	indeg := map[string]int{}
	for _, t := range tasks {
		byID[t.ID] = t
		if _, ok := indeg[t.ID]; !ok {
			indeg[t.ID] = 0
		}
	}
	for _, t := range tasks {
		for _, d := range t.DependsOn {
			if _, ok := byID[d]; !ok {
				return nil, fmt.Errorf("task %s depends on unknown %s", t.ID, d)
			}
			indeg[t.ID]++
		}
	}
	var ready []string
	for id, n := range indeg {
		if n == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	var out []*Task
	visited := map[string]bool{}
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		visited[id] = true
		out = append(out, byID[id])
		// any task whose deps are all visited becomes ready
		var next []string
		for _, t := range tasks {
			if visited[t.ID] {
				continue
			}
			allIn := true
			for _, d := range t.DependsOn {
				if !visited[d] {
					allIn = false
					break
				}
			}
			if allIn {
				next = append(next, t.ID)
			}
		}
		sort.Strings(next)
		// de-duplicate against ready
		seen := map[string]bool{}
		for _, r := range ready {
			seen[r] = true
		}
		for _, id := range next {
			if !seen[id] {
				ready = append(ready, id)
				seen[id] = true
			}
		}
	}
	if len(out) != len(tasks) {
		return nil, errors.New("cycle detected in task dependency graph")
	}
	return out, nil
}
