// Package lane is the Lane aggregate: a directed, mode-specific connection
// between two nodes with a nominal transit time. Its scheduled departures
// belong to linehaul-transportation.
//
// States: OPEN, CLOSED; transition OPEN -> CLOSED (CloseLane); no reopen, a
// new lane is opened instead. Invariants: origin and destination differ
// (ErrLaneLoop); the (origin type, destination type) pair is in the model's
// lane_rules (ErrLaneTypePairNotAllowed); both ends are ACTIVE when opening
// (ErrLaneEndpointNotActive); at most one OPEN lane per (origin, destination,
// mode) (ErrDuplicateLane, raised by the repository, ADR 0002 A10).
package lane

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// The Lane invariants of the model, by the model's names.
var (
	// ErrLaneLoop: origin and destination differ (422).
	ErrLaneLoop = errors.New("lane loop")
	// ErrLaneTypePairNotAllowed: the type pair is not in lane_rules.allowed (422).
	ErrLaneTypePairNotAllowed = errors.New("lane type pair not allowed")
	// ErrLaneEndpointNotActive: both ends must be ACTIVE when opening (409).
	ErrLaneEndpointNotActive = errors.New("lane endpoint not active")
	// ErrDuplicateLane: at most one OPEN lane per (origin, destination, mode)
	// (409). Raised by the repository on its partial unique index.
	ErrDuplicateLane = errors.New("duplicate lane")
)

// ID identifies a lane (laneId).
type ID string

// Mode is the transport mode of a lane.
type Mode string

// The two lane modes of the model.
const (
	ModeGround Mode = "GROUND"
	ModeAir    Mode = "AIR"
)

// Valid reports whether m is GROUND or AIR.
func (m Mode) Valid() bool { return m == ModeGround || m == ModeAir }

// Status is the lifecycle state of a lane.
type Status string

// The two lane states.
const (
	StatusOpen   Status = "OPEN"
	StatusClosed Status = "CLOSED"
)

// MaxTransitMinutes is the Postgres integer ceiling for a nominal transit.
const MaxTransitMinutes = 2147483647

// Pair is an (origin type, destination type) pair of lane_rules.
type Pair struct {
	Origin      node.Type
	Destination node.Type
}

// allowedPairs embeds the model's lane_rules.allowed in the model's order.
// TestAllowedPairsPinTheModel pins every row.
var allowedPairs = []Pair{
	{node.FulfillmentCenter, node.SortationCenter},
	{node.FulfillmentCenter, node.DeliveryStation},
	{node.FulfillmentCenter, node.AirHub},
	{node.SortationCenter, node.SortationCenter},
	{node.SortationCenter, node.DeliveryStation},
	{node.SortationCenter, node.AirHub},
	{node.AirHub, node.AirHub},
	{node.AirHub, node.SortationCenter},
	{node.AirHub, node.DeliveryStation},
	{node.InboundCrossDock, node.FulfillmentCenter},
}

// AllowedPairs returns the model's lane_rules.allowed.
func AllowedPairs() []Pair { return slices.Clone(allowedPairs) }

// PairAllowed reports whether a lane may run from an origin of type o to a
// destination of type d.
func PairAllowed(o, d node.Type) bool { return slices.Contains(allowedPairs, Pair{o, d}) }

// Lane is the aggregate root.
type Lane struct {
	id          ID
	origin      node.ID
	destination node.ID
	mode        Mode
	transit     int
	status      Status
	openedAt    time.Time
	closedAt    time.Time
	version     int64
}

// Snapshot is the persisted form of a lane. Restore validates it.
type Snapshot struct {
	ID                    ID
	OriginNodeID          node.ID
	DestinationNodeID     node.ID
	Mode                  Mode
	NominalTransitMinutes int
	Status                Status
	OpenedAt              time.Time
	ClosedAt              time.Time
	Version               int64
}

// NewLane is the input of Open.
type NewLane struct {
	ID                    ID
	Mode                  Mode
	NominalTransitMinutes int
}

// Open creates an OPEN lane at version 1 and raises LaneOpened. origin and
// destination are the Node aggregates the use case loaded (read only); nil is
// treated as an endpoint that is not ACTIVE.
func Open(in NewLane, origin, destination *node.Node, now time.Time) (*Lane, []shared.Event, error) {
	if !shared.ValidID(string(in.ID)) {
		return nil, nil, shared.Invalid("laneId %q is not a valid id", in.ID)
	}
	if !in.Mode.Valid() {
		return nil, nil, shared.Invalid("mode %q is not GROUND or AIR", in.Mode)
	}
	if in.NominalTransitMinutes < 1 || in.NominalTransitMinutes > MaxTransitMinutes {
		return nil, nil, shared.Invalid("nominalTransitMinutes must be between 1 and %d", MaxTransitMinutes)
	}
	if origin == nil || destination == nil {
		return nil, nil, fmt.Errorf("%w: both nodes must be loaded", ErrLaneEndpointNotActive)
	}
	if origin.ID() == destination.ID() {
		return nil, nil, fmt.Errorf("%w: %s", ErrLaneLoop, origin.ID())
	}
	if !PairAllowed(origin.Type(), destination.Type()) {
		return nil, nil, fmt.Errorf("%w: %s to %s", ErrLaneTypePairNotAllowed, origin.Type(), destination.Type())
	}
	for _, end := range []*node.Node{origin, destination} {
		if end.Status() != node.StatusActive {
			return nil, nil, fmt.Errorf("%w: node %s is %s", ErrLaneEndpointNotActive, end.ID(), end.Status())
		}
	}
	l := &Lane{
		id: in.ID, origin: origin.ID(), destination: destination.ID(), mode: in.Mode,
		transit: in.NominalTransitMinutes, status: StatusOpen, openedAt: shared.UTC(now), version: 1,
	}
	ev := LaneOpened{
		Header: shared.Header{Version: l.version, At: l.openedAt}, LaneID: l.id, OriginNodeID: l.origin,
		DestinationNodeID: l.destination, Mode: l.mode, NominalTransitMinutes: l.transit,
	}
	return l, []shared.Event{ev}, nil
}

// Restore rebuilds a persisted lane, validating every invariant.
func Restore(s Snapshot) (*Lane, error) {
	if err := checkSnapshot(s); err != nil {
		return nil, shared.Corrupt("lane %q: %v", s.ID, err)
	}
	return &Lane{
		id: s.ID, origin: s.OriginNodeID, destination: s.DestinationNodeID, mode: s.Mode,
		transit: s.NominalTransitMinutes, status: s.Status, openedAt: shared.UTC(s.OpenedAt),
		closedAt: shared.UTC(s.ClosedAt), version: s.Version,
	}, nil
}

func checkSnapshot(s Snapshot) error {
	if !shared.ValidID(string(s.ID)) || !shared.ValidID(string(s.OriginNodeID)) || !shared.ValidID(string(s.DestinationNodeID)) {
		return errors.New("laneId, originNodeId and destinationNodeId must be valid ids")
	}
	if s.OriginNodeID == s.DestinationNodeID {
		return ErrLaneLoop
	}
	if !s.Mode.Valid() {
		return fmt.Errorf("mode %q is not GROUND or AIR", s.Mode)
	}
	if s.NominalTransitMinutes < 1 || s.NominalTransitMinutes > MaxTransitMinutes {
		return errors.New("nominalTransitMinutes is out of range")
	}
	if s.OpenedAt.IsZero() || s.Version < 1 {
		return errors.New("openedAt and a version of at least 1 are required")
	}
	switch s.Status {
	case StatusOpen:
		if !s.ClosedAt.IsZero() {
			return errors.New("an OPEN lane has no closedAt")
		}
	case StatusClosed:
		if s.ClosedAt.IsZero() {
			return errors.New("a CLOSED lane needs closedAt")
		}
	default:
		return fmt.Errorf("status %q is not OPEN or CLOSED", s.Status)
	}
	return nil
}

// Close moves an OPEN lane to CLOSED and raises LaneClosed. Closing needs no
// ACTIVE endpoint.
func (l *Lane) Close(now time.Time) ([]shared.Event, error) {
	if l.status != StatusOpen {
		return nil, fmt.Errorf("%w: lane %s is %s", shared.ErrIllegalTransition, l.id, l.status)
	}
	l.status = StatusClosed
	l.closedAt = shared.UTC(now)
	l.version++
	ev := LaneClosed{Header: shared.Header{Version: l.version, At: l.closedAt}, LaneID: l.id, ClosedAt: l.closedAt}
	return []shared.Event{ev}, nil
}

// ID returns the lane id.
func (l *Lane) ID() ID { return l.id }

// OriginNodeID returns the origin node.
func (l *Lane) OriginNodeID() node.ID { return l.origin }

// DestinationNodeID returns the destination node.
func (l *Lane) DestinationNodeID() node.ID { return l.destination }

// Mode returns the transport mode.
func (l *Lane) Mode() Mode { return l.mode }

// NominalTransitMinutes returns the nominal transit time.
func (l *Lane) NominalTransitMinutes() int { return l.transit }

// Status returns OPEN or CLOSED.
func (l *Lane) Status() Status { return l.status }

// OpenedAt returns when the lane was opened.
func (l *Lane) OpenedAt() time.Time { return l.openedAt }

// ClosedAt returns when the lane was closed (zero while OPEN).
func (l *Lane) ClosedAt() time.Time { return l.closedAt }

// Version returns the aggregate version.
func (l *Lane) Version() int64 { return l.version }

// Snapshot returns the persisted form.
func (l *Lane) Snapshot() Snapshot {
	return Snapshot{
		ID: l.id, OriginNodeID: l.origin, DestinationNodeID: l.destination, Mode: l.mode,
		NominalTransitMinutes: l.transit, Status: l.status, OpenedAt: l.openedAt, ClosedAt: l.closedAt, Version: l.version,
	}
}
