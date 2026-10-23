// Package ids is the adapter of ports.IDGenerator: a kind prefix plus a UUID
// v4 ("node-<uuid>", "lane-<uuid>", "region-<uuid>"). Every id passes
// shared.ValidID (at most 64 characters from A-Z a-z 0-9 . _ : -).
package ids

import (
	"github.com/google/uuid"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// UUID mints ids as "<kind>-<uuid v4>".
type UUID struct{}

var _ ports.IDGenerator = UUID{}

// NewNodeID implements ports.IDGenerator.
func (UUID) NewNodeID() node.ID { return node.ID("node-" + uuid.NewString()) }

// NewLaneID implements ports.IDGenerator.
func (UUID) NewLaneID() lane.ID { return lane.ID("lane-" + uuid.NewString()) }

// NewRegionID implements ports.IDGenerator.
func (UUID) NewRegionID() region.ID { return region.ID("region-" + uuid.NewString()) }
