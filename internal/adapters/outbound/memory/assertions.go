package memory

import (
	"github.com/claudioed/node-topology/internal/application/ports"
)

// The doubles implement the out ports.
var (
	_ ports.NodeRepository        = (*NodeRepository)(nil)
	_ ports.LaneRepository        = (*LaneRepository)(nil)
	_ ports.ServiceAreaRepository = (*ServiceAreaRepository)(nil)
	_ ports.RegionRepository      = (*RegionRepository)(nil)
	_ ports.EventOutbox           = (*Outbox)(nil)
	_ ports.UnitOfWork            = (*UnitOfWork)(nil)
	_ ports.Clock                 = (*Clock)(nil)
	_ ports.IDGenerator           = (*SequentialIDs)(nil)
)
