// Package repository holds the non-interface vocabulary of the repository
// ports: the list filters. It has no behaviour; the interfaces are in
// internal/application/ports.
package repository

import (
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// Window is the keyset window of a list: only rows whose id sorts after
// AfterID (empty = from the start), at most Limit of them (0 = no limit).
// Every list is ordered by id ascending, the unique sort key.
type Window struct {
	AfterID string
	Limit   int
}

// Apply cuts rows (already ordered by id) to the window.
func Apply[T any](w Window, rows []T, id func(T) string) []T {
	if w.AfterID != "" {
		i := 0
		for i < len(rows) && id(rows[i]) <= w.AfterID {
			i++
		}
		rows = rows[i:]
	}
	if w.Limit > 0 && len(rows) > w.Limit {
		rows = rows[:w.Limit]
	}
	return rows
}

// RegionFilter windows ListRegions.
type RegionFilter struct{ Window }

// NodeFilter narrows ListNodes. A zero field does not filter; the set fields
// combine with AND.
type NodeFilter struct {
	Type     node.Type
	RegionID region.ID
	Status   node.Status
	Window
}

// LaneFilter narrows ListLanes. A zero field does not filter; the set fields
// combine with AND.
type LaneFilter struct {
	Origin      node.ID
	Destination node.ID
	Window
}
