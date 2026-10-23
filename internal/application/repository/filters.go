// Package repository holds the non-interface vocabulary of the repository
// ports: the list filters. It has no behaviour; the interfaces are in
// internal/application/ports.
package repository

import (
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// NodeFilter narrows ListNodes. A zero field does not filter; the set fields
// combine with AND.
type NodeFilter struct {
	Type     node.Type
	RegionID region.ID
	Status   node.Status
}

// LaneFilter narrows ListLanes. A zero field does not filter; the set fields
// combine with AND.
type LaneFilter struct {
	Origin      node.ID
	Destination node.ID
}
