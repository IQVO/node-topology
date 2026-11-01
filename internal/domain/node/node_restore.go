package node

import (
	"errors"
	"fmt"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

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
