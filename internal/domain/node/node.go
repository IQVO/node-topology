package node

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// ID identifies a node (nodeId).
type ID string

// VendorRef names a vendor's own code for a node.
type VendorRef struct {
	VendorID       string
	VendorNodeCode string
}

// GeoLocation is where the building is. Lat and Lon are both set or both nil.
type GeoLocation struct {
	PostalCode  string
	CountryCode string
	Lat         *float64
	Lon         *float64
}

func (g GeoLocation) clone() GeoLocation {
	g.Lat, g.Lon = cloneFloat(g.Lat), cloneFloat(g.Lon)
	return g
}

func cloneFloat(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := *f
	return &v
}

// MaxPostalCode is the longest postal code.
const MaxPostalCode = 20

// Node is the aggregate root: one physical building of the network.
type Node struct {
	id             ID
	typ            Type
	subtype        Subtype
	operator       Operator
	vendor         *VendorRef
	servingStation ID
	regionID       region.ID
	timezone       string
	location       GeoLocation
	status         Status
	profile        *OperationalProfile
	activatedAt    time.Time
	suspendedAt    time.Time
	suspendReason  string
	retiredAt      time.Time
	version        int64
}

// Snapshot is the persisted form of a node. Restore validates it. Only the
// CURRENT profile revision is held; the repository stores every revision as
// its own immutable row (ADR 0003).
type Snapshot struct {
	ID                   ID
	Type                 Type
	Subtype              Subtype
	Operator             Operator
	Vendor               *VendorRef
	ServingStationNodeID ID
	RegionID             region.ID
	Timezone             string
	Location             GeoLocation
	Status               Status
	Profile              *OperationalProfile
	ActivatedAt          time.Time
	SuspendedAt          time.Time
	SuspendReason        string
	RetiredAt            time.Time
	Version              int64
}

// Registration is the input of RegisterNode. ServingStationNodeID is required
// for a PICKUP_POINT and refused for every other type (ADR 0017).
type Registration struct {
	ID                   ID
	Type                 Type
	Subtype              Subtype
	Operator             Operator
	Vendor               *VendorRef
	ServingStationNodeID ID
	RegionID             region.ID
	Timezone             string
	Location             GeoLocation
}

// Register creates a node in PROPOSED at version 1 and raises NodeRegistered.
// reg is the Region the use case loaded for in.RegionID; nil, or a region with
// another id, is region.ErrUnknownRegion. station is the serving delivery
// station of a PICKUP_POINT: nil unless the type is PICKUP_POINT, and it must
// be an ACTIVE DELIVERY_STATION - the same invariant the service area
// enforces (ADR 0017). Capabilities are derived from the type.
func Register(in Registration, reg *region.Region, station *Node, now time.Time) (*Node, []shared.Event, error) {
	if err := checkEnums(in.Type, in.Operator); err != nil {
		return nil, nil, shared.Invalid("%v", err)
	}
	if !in.Type.AllowsOperator(in.Operator) {
		return nil, nil, fmt.Errorf("%w: %s cannot be run by %s", ErrOperatorNotAllowedForNodeType, in.Type, in.Operator)
	}
	if err := checkServingStation(in.Type, in.ServingStationNodeID, station); err != nil {
		return nil, nil, err // already the ErrServingStationNotAllowed chain
	}
	if in.Type == PickupPoint && !labellable(in.Location.PostalCode) {
		return nil, nil, fmt.Errorf("%w: %q", ErrPointPrefixNotLabellable, in.Location.PostalCode)
	}
	s := Snapshot{
		ID: in.ID, Type: in.Type, Subtype: in.Subtype, Operator: in.Operator, Vendor: in.Vendor,
		ServingStationNodeID: in.ServingStationNodeID,
		RegionID:             in.RegionID, Timezone: in.Timezone, Location: in.Location, Status: StatusProposed, Version: 1,
	}
	if err := checkIdentity(s); err != nil {
		return nil, nil, shared.Invalid("%v", err)
	}
	if reg == nil || reg.ID() != in.RegionID {
		return nil, nil, fmt.Errorf("%w: %q", region.ErrUnknownRegion, in.RegionID)
	}
	if in.Subtype != "" && !in.Type.AllowsSubtype(in.Subtype) {
		return nil, nil, fmt.Errorf("%w: %q for %s", ErrSubtypeNotAllowed, in.Subtype, in.Type)
	}
	n := fromSnapshot(s)
	ev := NodeRegistered{
		Header: shared.Header{Version: n.version, At: shared.UTC(now)},
		NodeID: n.id, NodeType: n.typ, Subtype: n.subtype, Operator: n.operator,
		ServingStationNodeID: n.servingStation,
		RegionID:             n.regionID, Timezone: n.timezone, PostalCode: n.location.PostalCode, CountryCode: n.location.CountryCode,
	}
	if n.vendor != nil {
		ev.VendorID, ev.VendorNodeCode = n.vendor.VendorID, n.vendor.VendorNodeCode
	}
	return n, []shared.Event{ev}, nil
}

func checkEnums(t Type, o Operator) error {
	if !t.Valid() {
		return fmt.Errorf("nodeType %q is not a node type", t)
	}
	if !o.Valid() {
		return fmt.Errorf("operator %q is not NETWORK, VENDOR or PARTNER", o)
	}
	return nil
}

// checkIdentity validates the registration values shared by Register and Restore.
func checkIdentity(s Snapshot) error {
	if !shared.ValidID(string(s.ID)) {
		return fmt.Errorf("nodeId %q is not a valid id", s.ID)
	}
	if !shared.ValidID(string(s.RegionID)) {
		return fmt.Errorf("regionId %q is not a valid id", s.RegionID)
	}
	if err := checkServingStation(s.Type, s.ServingStationNodeID, nil); err != nil {
		return err
	}
	if s.Type == PickupPoint && !labellable(s.Location.PostalCode) {
		return fmt.Errorf("%w: %q", ErrPointPrefixNotLabellable, s.Location.PostalCode)
	}
	if err := checkVendor(s.Operator, s.Vendor); err != nil {
		return err
	}
	if !validTimezone(s.Timezone) {
		return fmt.Errorf("timezone %q must look like an IANA name such as America/Sao_Paulo", s.Timezone)
	}
	return checkLocation(s.Location)
}

func checkVendor(o Operator, v *VendorRef) error {
	if o == OperatorNetwork {
		if v != nil {
			return errors.New("a NETWORK node carries no vendorRef")
		}
		return nil
	}
	if v == nil || !shared.ValidText(v.VendorID) || !shared.ValidText(v.VendorNodeCode) {
		return errors.New("a VENDOR or PARTNER node needs a vendorRef with vendorId and vendorNodeCode")
	}
	return nil
}

// checkServingStation enforces the PICKUP_POINT-only serving station (ADR
// 0017). Restore passes nil: the persisted shape is already constrained by the
// presence rule, and the station's own status is re-checked by the projection.
func checkServingStation(t Type, id ID, station *Node) error {
	if t != PickupPoint {
		if id != "" {
			return fmt.Errorf("%w: %s carries no serving station", ErrServingStationNotAllowed, t)
		}
		return nil
	}
	if id == "" || !shared.ValidID(string(id)) {
		return fmt.Errorf("%w: a PICKUP_POINT needs a servingStationNodeId", ErrServingStationNotAllowed)
	}
	if station == nil {
		return nil // restore path: the shape rule alone
	}
	if station.typ != DeliveryStation || station.status != StatusActive {
		return fmt.Errorf("%w: %s is %s %s", ErrServingStationNotAllowed, station.id, station.status, station.typ)
	}
	if station.id != id {
		return fmt.Errorf("%w: the loaded station is %s, not %s", ErrServingStationNotAllowed, station.id, id)
	}
	return nil
}

// labellable reports whether a postal code yields a prefix the label charset
// accepts (A-Z 0-9, the shipping label rule): a pickup point's own code rides
// its labels (ADR 0017).
func labellable(postalCode string) bool {
	if postalCode == "" {
		return false
	}
	for _, r := range postalCode {
		if !labellableRune(r) {
			return false
		}
	}
	return true
}

// labellableRune reports whether r is an uppercase letter or a digit.
func labellableRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func checkLocation(g GeoLocation) error {
	if g.PostalCode == "" || g.PostalCode != strings.TrimSpace(g.PostalCode) || len(g.PostalCode) > MaxPostalCode {
		return fmt.Errorf("postalCode must be a trimmed string of 1 to %d characters", MaxPostalCode)
	}
	if len(g.CountryCode) != 2 || g.CountryCode[0] < 'A' || g.CountryCode[0] > 'Z' || g.CountryCode[1] < 'A' || g.CountryCode[1] > 'Z' {
		return errors.New("countryCode must be two uppercase letters")
	}
	if (g.Lat == nil) != (g.Lon == nil) {
		return errors.New("lat and lon are given together or not at all")
	}
	if g.Lat != nil && (*g.Lat < -90 || *g.Lat > 90 || *g.Lon < -180 || *g.Lon > 180) {
		return errors.New("lat must be within [-90, 90] and lon within [-180, 180]")
	}
	return nil
}

// MaxReason is the longest suspension reason, in characters.
const MaxReason = 500

// ID returns the node id.
func (n *Node) ID() ID { return n.id }

// Type returns the node type.
func (n *Node) Type() Type { return n.typ }

// Subtype returns the subtype, empty when none.
func (n *Node) Subtype() Subtype { return n.subtype }

// Operator returns who runs the building.
func (n *Node) Operator() Operator { return n.operator }

// ServingStationNodeID returns the serving delivery station of a PICKUP_POINT
// (empty for every other type).
func (n *Node) ServingStationNodeID() ID { return n.servingStation }

// Vendor returns the vendor reference of a VENDOR node.
func (n *Node) Vendor() (VendorRef, bool) {
	if n.vendor == nil {
		return VendorRef{}, false
	}
	return *n.vendor, true
}

// RegionID returns the node's region.
func (n *Node) RegionID() region.ID { return n.regionID }

// Timezone returns the IANA time zone of the building.
func (n *Node) Timezone() string { return n.timezone }

// Location returns a copy of the building's location.
func (n *Node) Location() GeoLocation { return n.location.clone() }

// Capabilities returns the capabilities derived from the node type.
func (n *Node) Capabilities() Capabilities { return n.typ.Capabilities() }

// Status returns the lifecycle state.
func (n *Node) Status() Status { return n.status }

// Profile returns a copy of the current operational profile, if one was declared.
func (n *Node) Profile() (OperationalProfile, bool) {
	if n.profile == nil {
		return OperationalProfile{}, false
	}
	return n.profile.clone(), true
}

// ProfileRevision returns the current profile revision, 0 when none.
func (n *Node) ProfileRevision() int {
	if n.profile == nil {
		return 0
	}
	return n.profile.Revision
}

// ActivatedAt returns when the node last became ACTIVE.
func (n *Node) ActivatedAt() time.Time { return n.activatedAt }

// SuspendedAt returns when the node became SUSPENDED (zero unless SUSPENDED or
// retired from SUSPENDED).
func (n *Node) SuspendedAt() time.Time { return n.suspendedAt }

// SuspendReason returns why the node was suspended.
func (n *Node) SuspendReason() string { return n.suspendReason }

// RetiredAt returns when the node became RETIRED.
func (n *Node) RetiredAt() time.Time { return n.retiredAt }

// Version returns the aggregate version.
func (n *Node) Version() int64 { return n.version }

// AcceptsDemand reports whether the node may take customer demand: only while
// ACTIVE and only with the OUTBOUND capability (business decision 2).
func (n *Node) AcceptsDemand() bool {
	return n.status == StatusActive && n.Capabilities().Outbound
}

// Snapshot returns the persisted form (pointers and slices are copies).
func (n *Node) Snapshot() Snapshot {
	s := Snapshot{
		ID: n.id, Type: n.typ, Subtype: n.subtype, Operator: n.operator,
		ServingStationNodeID: n.servingStation, RegionID: n.regionID,
		Timezone: n.timezone, Location: n.location.clone(), Status: n.status,
		ActivatedAt: n.activatedAt, SuspendedAt: n.suspendedAt, SuspendReason: n.suspendReason,
		RetiredAt: n.retiredAt, Version: n.version,
	}
	if v, ok := n.Vendor(); ok {
		s.Vendor = &v
	}
	if p, ok := n.Profile(); ok {
		s.Profile = &p
	}
	return s
}
