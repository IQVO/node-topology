package usecases

import (
	"context"
	"time"

	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// RegisterNodeCommand is the input of RegisterNode.
type RegisterNodeCommand struct {
	Type     node.Type
	Subtype  node.Subtype
	Operator node.Operator
	Vendor   *node.VendorRef
	RegionID region.ID
	Timezone string
	Location node.GeoLocation
}

// RegisterNode registers a node in PROPOSED.
type RegisterNode struct{ writer }

// NewRegisterNode builds the use case.
func NewRegisterNode(d Deps) *RegisterNode { return &RegisterNode{writer{d}} }

// Execute mints the node id, loads the region (a missing one reaches the
// domain as nil, which is region.ErrUnknownRegion), registers the node, checks
// that its vendor code is free (node.ErrVendorNodeCodeTaken) and saves it with
// NodeRegistered. The domain's value checks come first, so an invalid command
// is never reported as a taken code.
func (uc *RegisterNode) Execute(ctx context.Context, cmd RegisterNodeCommand) (views.Node, error) {
	return transact(ctx, uc.UoW, func(ctx context.Context) (views.Node, error) {
		reg, err := optionalRegion(ctx, uc.Regions, cmd.RegionID)
		if err != nil {
			return views.Node{}, err
		}
		n, events, err := node.Register(node.Registration{
			ID: uc.IDs.NewNodeID(), Type: cmd.Type, Subtype: cmd.Subtype, Operator: cmd.Operator,
			Vendor: cmd.Vendor, RegionID: cmd.RegionID, Timezone: cmd.Timezone, Location: cmd.Location,
		}, reg, uc.now())
		if err != nil {
			return views.Node{}, err
		}
		if err := uc.checkVendorCodeFree(ctx, n); err != nil {
			return views.Node{}, err
		}
		if err := uc.Nodes.Save(ctx, n); err != nil {
			return views.Node{}, err
		}
		if err := uc.publish(ctx, events); err != nil {
			return views.Node{}, err
		}
		return views.NodeFrom(n), nil
	})
}

// checkVendorCodeFree is the friendly pre-check of the unique index on
// (vendorId, vendorNodeCode); the repository enforces it for real on Save.
func (uc *RegisterNode) checkVendorCodeFree(ctx context.Context, n *node.Node) error {
	ref, vendored := n.Vendor()
	if !vendored {
		return nil
	}
	_, err := uc.Nodes.FindByVendorRef(ctx, ref.VendorID, ref.VendorNodeCode)
	taken, err := found(err)
	if err != nil {
		return err
	}
	if taken {
		return node.ErrVendorNodeCodeTaken
	}
	return nil
}

// DeclareOperationalProfileCommand is the input of DeclareOperationalProfile.
type DeclareOperationalProfileCommand struct {
	NodeID      node.ID
	Declaration node.ProfileDeclaration
}

// DeclareOperationalProfile appends a profile revision to a node.
type DeclareOperationalProfile struct{ writer }

// NewDeclareOperationalProfile builds the use case.
func NewDeclareOperationalProfile(d Deps) *DeclareOperationalProfile {
	return &DeclareOperationalProfile{writer{d}}
}

// Execute declares the revision and returns the node with its current profile.
// The node repository stores the revision as its own row.
func (uc *DeclareOperationalProfile) Execute(ctx context.Context, cmd DeclareOperationalProfileCommand) (views.Node, error) {
	return uc.mutateNode(ctx, cmd.NodeID, func(n *node.Node, now time.Time) ([]shared.Event, error) {
		return n.DeclareProfile(cmd.Declaration, now)
	})
}

// ActivateNode moves a PROPOSED or SUSPENDED node to ACTIVE.
type ActivateNode struct{ writer }

// NewActivateNode builds the use case.
func NewActivateNode(d Deps) *ActivateNode { return &ActivateNode{writer{d}} }

// Execute activates the node.
func (uc *ActivateNode) Execute(ctx context.Context, id node.ID) (views.Node, error) {
	return uc.mutateNode(ctx, id, func(n *node.Node, now time.Time) ([]shared.Event, error) {
		return n.Activate(now)
	})
}

// SuspendNode moves an ACTIVE node to SUSPENDED.
type SuspendNode struct{ writer }

// NewSuspendNode builds the use case.
func NewSuspendNode(d Deps) *SuspendNode { return &SuspendNode{writer{d}} }

// Execute suspends the node for the reason. Its lanes and service areas are
// left as they are (SuspendedEndpointsUnusable).
func (uc *SuspendNode) Execute(ctx context.Context, id node.ID, reason string) (views.Node, error) {
	return uc.mutateNode(ctx, id, func(n *node.Node, now time.Time) ([]shared.Event, error) {
		return n.Suspend(reason, now)
	})
}

// RetireNode moves an ACTIVE or SUSPENDED node to RETIRED.
type RetireNode struct{ writer }

// NewRetireNode builds the use case.
func NewRetireNode(d Deps) *RetireNode { return &RetireNode{writer{d}} }

// Execute retires the node, which is terminal.
func (uc *RetireNode) Execute(ctx context.Context, id node.ID) (views.Node, error) {
	return uc.mutateNode(ctx, id, func(n *node.Node, now time.Time) ([]shared.Event, error) {
		return n.Retire(now)
	})
}
