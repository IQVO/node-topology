package lane

import (
	"time"

	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

const entity = "lane"

// LaneOpened is raised by Open. Its fields are the model's data list.
type LaneOpened struct {
	shared.Header
	LaneID                ID
	OriginNodeID          node.ID
	DestinationNodeID     node.ID
	Mode                  Mode
	NominalTransitMinutes int
}

// EventName returns "LaneOpened".
func (LaneOpened) EventName() string { return "LaneOpened" }

// Entity returns "lane".
func (LaneOpened) Entity() string { return entity }

// SubjectID returns the laneId.
func (e LaneOpened) SubjectID() string { return string(e.LaneID) }

// LaneClosed is raised by Close.
type LaneClosed struct {
	shared.Header
	LaneID   ID
	ClosedAt time.Time
}

// EventName returns "LaneClosed".
func (LaneClosed) EventName() string { return "LaneClosed" }

// Entity returns "lane".
func (LaneClosed) Entity() string { return entity }

// SubjectID returns the laneId.
func (e LaneClosed) SubjectID() string { return string(e.LaneID) }

var (
	_ shared.Event = LaneOpened{}
	_ shared.Event = LaneClosed{}
)
