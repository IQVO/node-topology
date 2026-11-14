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

// LaneCostDeclared is raised by DeclareCost (ADR 0014): the planner's
// declared marginal cost of moving ONE package-unit across the lane on one
// departure, in integer cents.
type LaneCostDeclared struct {
	shared.Header
	LaneID           ID
	CostPerUnitCents int64
	DeclaredAt       time.Time
}

// EventName returns "LaneCostDeclared".
func (LaneCostDeclared) EventName() string { return "LaneCostDeclared" }

// Entity returns "lane".
func (LaneCostDeclared) Entity() string { return entity }

// SubjectID returns the laneId.
func (e LaneCostDeclared) SubjectID() string { return string(e.LaneID) }

// LaneEmissionDeclared is raised by DeclareEmission (ADR 0018): the
// planner's declared carbon emission of moving ONE package-unit across
// the lane on one departure, in grams of CO2-equivalent.
type LaneEmissionDeclared struct {
	shared.Header
	LaneID       ID
	GCo2ePerUnit int64
	DeclaredAt   time.Time
}

// EventName returns "LaneEmissionDeclared".
func (LaneEmissionDeclared) EventName() string { return "LaneEmissionDeclared" }

// Entity returns "lane".
func (LaneEmissionDeclared) Entity() string { return entity }

// SubjectID returns the laneId.
func (e LaneEmissionDeclared) SubjectID() string { return string(e.LaneID) }

var (
	_ shared.Event = LaneOpened{}
	_ shared.Event = LaneClosed{}
	_ shared.Event = LaneCostDeclared{}
	_ shared.Event = LaneEmissionDeclared{}
)
