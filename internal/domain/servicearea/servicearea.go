// Package servicearea is the ServiceArea aggregate: which delivery station
// (and therefore which region) serves a postal prefix, versioned by
// assignmentVersion.
//
// One state, ASSIGNED (initial), and one transition ASSIGNED -> ASSIGNED
// (AssignServiceArea): a prefix is assigned, then reassigned under a strictly
// higher version. There is no unassign. Invariants: the assigned node is an
// ACTIVE DELIVERY_STATION (ErrNotADeliveryStation); the version strictly
// increases (ErrStaleAssignmentVersion); the region is a defined Region and
// equals the station's region (region.ErrUnknownRegion).
package servicearea

import (
	"errors"
	"fmt"
	"time"

	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// The ServiceArea invariants of the model, by the model's names.
var (
	// ErrNotADeliveryStation: the assigned node is an ACTIVE DELIVERY_STATION (409).
	ErrNotADeliveryStation = errors.New("not a delivery station")
	// ErrStaleAssignmentVersion: the assignment version strictly increases (409).
	ErrStaleAssignmentVersion = errors.New("stale assignment version")
)

const (
	// MaxPrefix is the longest postal prefix.
	MaxPrefix = 16
	// MaxAssignmentVersion is the Postgres integer ceiling for a version.
	MaxAssignmentVersion = 2147483647
)

// PostalPrefix identifies a service area. It is 1 to MaxPrefix characters from
// A-Z 0-9 -, matched exactly and never normalised (ADR 0002, A11).
type PostalPrefix string

// Valid reports whether p is a well-formed postal prefix.
func (p PostalPrefix) Valid() bool {
	if p == "" || len(p) > MaxPrefix {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c < 'A' && c != '-' && (c < '0' || c > '9') || c > 'Z' {
			return false
		}
	}
	return true
}

// ServiceArea is the aggregate root.
type ServiceArea struct {
	prefix     PostalPrefix
	station    node.ID
	region     region.ID
	assignment int
	assignedAt time.Time
	version    int64
}

// Snapshot is the persisted form of a service area. Restore validates it.
type Snapshot struct {
	PostalPrefix          PostalPrefix
	DeliveryStationNodeID node.ID
	RegionID              region.ID
	AssignmentVersion     int
	AssignedAt            time.Time
	Version               int64
}

// Assign creates the first assignment of a prefix (aggregate version 1) and
// raises ServiceAreaAssigned. station and reg are the aggregates the use case
// loaded (read only).
func Assign(prefix PostalPrefix, assignmentVersion int, station *node.Node, reg *region.Region, now time.Time) (*ServiceArea, []shared.Event, error) {
	if !prefix.Valid() {
		return nil, nil, shared.Invalid("postalPrefix %q must be 1 to %d characters from A-Z 0-9 -", prefix, MaxPrefix)
	}
	sa := &ServiceArea{prefix: prefix}
	events, err := sa.assign(assignmentVersion, station, reg, now)
	if err != nil {
		return nil, nil, err
	}
	return sa, events, nil
}

// Reassign serves the prefix from another (or the same) station under a
// strictly higher assignment version and raises ServiceAreaAssigned.
func (s *ServiceArea) Reassign(assignmentVersion int, station *node.Node, reg *region.Region, now time.Time) ([]shared.Event, error) {
	return s.assign(assignmentVersion, station, reg, now)
}

// assign applies one AssignServiceArea. It changes the receiver only on success.
func (s *ServiceArea) assign(assignmentVersion int, station *node.Node, reg *region.Region, now time.Time) ([]shared.Event, error) {
	if assignmentVersion < 1 || assignmentVersion > MaxAssignmentVersion {
		return nil, shared.Invalid("assignmentVersion must be between 1 and %d", MaxAssignmentVersion)
	}
	if station == nil || station.Type() != node.DeliveryStation || station.Status() != node.StatusActive {
		return nil, fmt.Errorf("%w: prefix %s", ErrNotADeliveryStation, s.prefix)
	}
	if assignmentVersion <= s.assignment {
		return nil, fmt.Errorf("%w: version %d, current %d", ErrStaleAssignmentVersion, assignmentVersion, s.assignment)
	}
	if reg == nil || reg.ID() != station.RegionID() {
		return nil, fmt.Errorf("%w: station %s is in region %q", region.ErrUnknownRegion, station.ID(), station.RegionID())
	}
	s.station, s.region, s.assignment = station.ID(), reg.ID(), assignmentVersion
	s.assignedAt = shared.UTC(now)
	s.version++
	ev := ServiceAreaAssigned{
		Header:       shared.Header{Version: s.version, At: s.assignedAt},
		PostalPrefix: s.prefix, DeliveryStationNodeID: s.station, RegionID: s.region, AssignmentVersion: s.assignment,
	}
	return []shared.Event{ev}, nil
}

// Restore rebuilds a persisted service area, validating every invariant.
func Restore(snap Snapshot) (*ServiceArea, error) {
	if err := checkSnapshot(snap); err != nil {
		return nil, shared.Corrupt("service area %q: %v", snap.PostalPrefix, err)
	}
	return &ServiceArea{
		prefix: snap.PostalPrefix, station: snap.DeliveryStationNodeID, region: snap.RegionID,
		assignment: snap.AssignmentVersion, assignedAt: shared.UTC(snap.AssignedAt), version: snap.Version,
	}, nil
}

func checkSnapshot(s Snapshot) error {
	if !s.PostalPrefix.Valid() {
		return errors.New("postalPrefix is malformed")
	}
	if !shared.ValidID(string(s.DeliveryStationNodeID)) || !shared.ValidID(string(s.RegionID)) {
		return errors.New("deliveryStationNodeId and regionId must be valid ids")
	}
	if s.AssignmentVersion < 1 || s.AssignmentVersion > MaxAssignmentVersion {
		return errors.New("assignmentVersion is out of range")
	}
	if s.AssignedAt.IsZero() || s.Version < 1 {
		return errors.New("assignedAt and a version of at least 1 are required")
	}
	return nil
}

// PostalPrefix returns the prefix.
func (s *ServiceArea) PostalPrefix() PostalPrefix { return s.prefix }

// DeliveryStationNodeID returns the station that serves the prefix.
func (s *ServiceArea) DeliveryStationNodeID() node.ID { return s.station }

// RegionID returns the region of the serving station.
func (s *ServiceArea) RegionID() region.ID { return s.region }

// AssignmentVersion returns the current assignment version.
func (s *ServiceArea) AssignmentVersion() int { return s.assignment }

// AssignedAt returns when the current assignment was made.
func (s *ServiceArea) AssignedAt() time.Time { return s.assignedAt }

// Version returns the aggregate version (one per saved assignment).
func (s *ServiceArea) Version() int64 { return s.version }

// Snapshot returns the persisted form.
func (s *ServiceArea) Snapshot() Snapshot {
	return Snapshot{
		PostalPrefix: s.prefix, DeliveryStationNodeID: s.station, RegionID: s.region,
		AssignmentVersion: s.assignment, AssignedAt: s.assignedAt, Version: s.version,
	}
}
