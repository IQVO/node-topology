package node

import (
	"fmt"
	"time"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

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
	// The capability gate runs before the value checks of ADR 0007, and only
	// here: Restore keeps accepting stored limits (ADR 0015).
	if d.Inbound != nil && !n.typ.Capabilities().Inbound {
		return nil, fmt.Errorf("%w: %s has no INBOUND capability", ErrInboundLimitsNotAllowed, n.typ)
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
