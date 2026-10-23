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
	id            ID
	typ           Type
	subtype       Subtype
	operator      Operator
	vendor        *VendorRef
	regionID      region.ID
	timezone      string
	location      GeoLocation
	status        Status
	profile       *OperationalProfile
	activatedAt   time.Time
	suspendedAt   time.Time
	suspendReason string
	retiredAt     time.Time
	version       int64
}

// Snapshot is the persisted form of a node. Restore validates it. Only the
// CURRENT profile revision is held; the repository stores every revision as
// its own immutable row (ADR 0003).
type Snapshot struct {
	ID            ID
	Type          Type
	Subtype       Subtype
	Operator      Operator
	Vendor        *VendorRef
	RegionID      region.ID
	Timezone      string
	Location      GeoLocation
	Status        Status
	Profile       *OperationalProfile
	ActivatedAt   time.Time
	SuspendedAt   time.Time
	SuspendReason string
	RetiredAt     time.Time
	Version       int64
}

// Registration is the input of RegisterNode.
type Registration struct {
	ID       ID
	Type     Type
	Subtype  Subtype
	Operator Operator
	Vendor   *VendorRef
	RegionID region.ID
	Timezone string
	Location GeoLocation
}

// Register creates a node in PROPOSED at version 1 and raises NodeRegistered.
// reg is the Region the use case loaded for in.RegionID; nil, or a region with
// another id, is region.ErrUnknownRegion. Capabilities are derived from the type.
func Register(in Registration, reg *region.Region, now time.Time) (*Node, []shared.Event, error) {
	if err := checkEnums(in.Type, in.Operator); err != nil {
		return nil, nil, shared.Invalid("%v", err)
	}
	if !in.Type.AllowsOperator(in.Operator) {
		return nil, nil, fmt.Errorf("%w: %s cannot be run by %s", ErrOperatorNotAllowedForNodeType, in.Type, in.Operator)
	}
	s := Snapshot{
		ID: in.ID, Type: in.Type, Subtype: in.Subtype, Operator: in.Operator, Vendor: in.Vendor,
		RegionID: in.RegionID, Timezone: in.Timezone, Location: in.Location, Status: StatusProposed, Version: 1,
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
		RegionID: n.regionID, Timezone: n.timezone, PostalCode: n.location.PostalCode, CountryCode: n.location.CountryCode,
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
		return fmt.Errorf("operator %q is not NETWORK or VENDOR", o)
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
		return errors.New("a VENDOR node needs a vendorRef with vendorId and vendorNodeCode")
	}
	return nil
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

// fromSnapshot copies a validated snapshot into a node.
func fromSnapshot(s Snapshot) *Node {
	n := &Node{
		id: s.ID, typ: s.Type, subtype: s.Subtype, operator: s.Operator, regionID: s.RegionID,
		timezone: s.Timezone, location: s.Location.clone(), status: s.Status,
		activatedAt: shared.UTC(s.ActivatedAt), suspendedAt: shared.UTC(s.SuspendedAt),
		suspendReason: s.SuspendReason, retiredAt: shared.UTC(s.RetiredAt), version: s.Version,
	}
	if s.Vendor != nil {
		v := *s.Vendor
		n.vendor = &v
	}
	if s.Profile != nil {
		p := s.Profile.clone()
		n.profile = &p
	}
	return n
}

// Restore rebuilds a persisted node, validating every invariant, so a corrupt
// row can never become a live aggregate.
func Restore(s Snapshot) (*Node, error) {
	if err := checkSnapshot(s); err != nil {
		return nil, shared.Corrupt("node %q: %v", s.ID, err)
	}
	return fromSnapshot(s), nil
}

func checkSnapshot(s Snapshot) error {
	if err := checkEnums(s.Type, s.Operator); err != nil {
		return err
	}
	if !s.Type.AllowsOperator(s.Operator) {
		return ErrOperatorNotAllowedForNodeType
	}
	if err := checkIdentity(s); err != nil {
		return err
	}
	if s.Subtype != "" && !s.Type.AllowsSubtype(s.Subtype) {
		return ErrSubtypeNotAllowed
	}
	if s.Version < 1 {
		return errors.New("version must be at least 1")
	}
	if err := checkLifecycle(s); err != nil {
		return err
	}
	if s.Profile == nil {
		return checkNoProfile(s)
	}
	if err := checkShape(*s.Profile); err != nil {
		return err
	}
	if err := checkCutoffs(*s.Profile); err != nil {
		return err
	}
	return checkProfileFits(s.Type, s.Operator, s.Status, *s.Profile)
}

func checkNoProfile(s Snapshot) error {
	if s.Status == StatusActive && s.Type == FulfillmentCenter {
		return ErrIncompleteOperationalProfile
	}
	return nil
}

// checkLifecycle checks that the timestamps match the status: PROPOSED has
// none, ACTIVE has activatedAt, SUSPENDED adds suspendedAt and a reason, RETIRED
// has activatedAt and retiredAt (and keeps the suspension of a node retired
// from SUSPENDED).
func checkLifecycle(s Snapshot) error {
	if !s.Status.Valid() {
		return fmt.Errorf("status %q is not a node state", s.Status)
	}
	activated, retired := !s.ActivatedAt.IsZero(), !s.RetiredAt.IsZero()
	if activated != (s.Status != StatusProposed) || retired != (s.Status == StatusRetired) {
		return fmt.Errorf("activatedAt and retiredAt do not match status %s", s.Status)
	}
	suspended := !s.SuspendedAt.IsZero() && shared.ValidTextN(s.SuspendReason, MaxReason)
	partial := !s.SuspendedAt.IsZero() || s.SuspendReason != ""
	ok := !partial
	switch s.Status {
	case StatusSuspended:
		ok = suspended
	case StatusRetired:
		ok = suspended || !partial
	}
	if !ok {
		return fmt.Errorf("suspendedAt and suspendReason do not match status %s", s.Status)
	}
	return nil
}

// checkProfileFits checks a profile against the node it belongs to.
func checkProfileFits(t Type, o Operator, st Status, p OperationalProfile) error {
	if p.DeclaredBy == DeclaredByVendor && o == OperatorNetwork {
		return shared.Invalid("a NETWORK node's profile cannot be declared by VENDOR")
	}
	if st == StatusActive && t == FulfillmentCenter && !p.Complete() {
		return ErrIncompleteOperationalProfile
	}
	if p.SortDwellMinutes != nil && t != SortationCenter && t != AirHub {
		return ErrSortDwellNotAllowed
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
		ID: n.id, Type: n.typ, Subtype: n.subtype, Operator: n.operator, RegionID: n.regionID,
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

// guard refuses a move the transition table does not allow.
func (n *Node) guard(to Status) error {
	if n.status == StatusRetired {
		return fmt.Errorf("%w: %s", ErrNodeRetired, n.id)
	}
	if !canMove(n.status, to) {
		return fmt.Errorf("%w: node %s is %s, cannot become %s", shared.ErrIllegalTransition, n.id, n.status, to)
	}
	return nil
}

func (n *Node) header(now time.Time) shared.Header {
	return shared.Header{Version: n.version, At: shared.UTC(now)}
}

// Activate moves a PROPOSED or SUSPENDED node to ACTIVE and raises
// NodeActivated. A FULFILLMENT_CENTER needs a complete current profile.
func (n *Node) Activate(now time.Time) ([]shared.Event, error) {
	if err := n.guard(StatusActive); err != nil {
		return nil, err
	}
	if n.typ == FulfillmentCenter && (n.profile == nil || !n.profile.Complete()) {
		return nil, fmt.Errorf("%w: node %s needs a cutoff and a handling time above zero", ErrIncompleteOperationalProfile, n.id)
	}
	n.status = StatusActive
	n.activatedAt = shared.UTC(now)
	n.suspendedAt, n.suspendReason = time.Time{}, ""
	n.version++
	return []shared.Event{NodeActivated{Header: n.header(now), NodeID: n.id, ActivatedAt: n.activatedAt}}, nil
}

// Suspend moves an ACTIVE node to SUSPENDED and raises NodeSuspended. Its lanes
// and service areas are left as they are (SuspendedEndpointsUnusable).
func (n *Node) Suspend(reason string, now time.Time) ([]shared.Event, error) {
	if err := n.guard(StatusSuspended); err != nil {
		return nil, err
	}
	if !shared.ValidTextN(reason, MaxReason) {
		return nil, shared.Invalid("reason must be a trimmed, non-blank string of at most %d characters", MaxReason)
	}
	n.status = StatusSuspended
	n.suspendedAt, n.suspendReason = shared.UTC(now), reason
	n.version++
	return []shared.Event{NodeSuspended{Header: n.header(now), NodeID: n.id, Reason: reason, SuspendedAt: n.suspendedAt}}, nil
}

// Retire moves an ACTIVE or SUSPENDED node to RETIRED, which is terminal, and
// raises NodeRetired.
func (n *Node) Retire(now time.Time) ([]shared.Event, error) {
	if err := n.guard(StatusRetired); err != nil {
		return nil, err
	}
	n.status = StatusRetired
	n.retiredAt = shared.UTC(now)
	n.version++
	return []shared.Event{NodeRetired{Header: n.header(now), NodeID: n.id, RetiredAt: n.retiredAt}}, nil
}

// DeclareProfile appends a new profile revision and raises
// NodeOperationalProfileDeclared. It changes no state and is accepted in
// PROPOSED, ACTIVE and SUSPENDED. Earlier revisions are never edited.
func (n *Node) DeclareProfile(d ProfileDeclaration, now time.Time) ([]shared.Event, error) {
	if n.status == StatusRetired {
		return nil, fmt.Errorf("%w: %s", ErrNodeRetired, n.id)
	}
	if d.Revision <= n.ProfileRevision() {
		return nil, fmt.Errorf("%w: revision %d, current %d", ErrStaleProfileRevision, d.Revision, n.ProfileRevision())
	}
	p, err := NewOperationalProfile(d, now)
	if err != nil {
		return nil, err
	}
	if err := checkProfileFits(n.typ, n.operator, n.status, p); err != nil {
		return nil, err
	}
	n.profile = &p
	n.version++
	return []shared.Event{profileDeclared(n.header(now), n.id, p.clone())}, nil
}
