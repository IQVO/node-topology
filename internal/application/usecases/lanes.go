package usecases

import (
	"context"

	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
)

// OpenLaneCommand is the input of OpenLane.
type OpenLaneCommand struct {
	OriginNodeID          node.ID
	DestinationNodeID     node.ID
	Mode                  lane.Mode
	NominalTransitMinutes int
}

// OpenLane opens a lane between two ACTIVE nodes.
type OpenLane struct{ writer }

// NewOpenLane builds the use case.
func NewOpenLane(d Deps) *OpenLane { return &OpenLane{writer{d}} }

// Execute loads both ends (an unknown one is ports.ErrNotFound), opens the
// lane, checks that no lane is OPEN for the same (origin, destination, mode)
// (lane.ErrDuplicateLane) and saves it with LaneOpened.
func (uc *OpenLane) Execute(ctx context.Context, cmd OpenLaneCommand) (views.Lane, error) {
	return transact(ctx, uc.UoW, func(ctx context.Context) (views.Lane, error) {
		origin, err := uc.Nodes.Get(ctx, cmd.OriginNodeID)
		if err != nil {
			return views.Lane{}, err
		}
		destination, err := uc.Nodes.Get(ctx, cmd.DestinationNodeID)
		if err != nil {
			return views.Lane{}, err
		}
		l, events, err := lane.Open(lane.NewLane{
			ID: uc.IDs.NewLaneID(), Mode: cmd.Mode, NominalTransitMinutes: cmd.NominalTransitMinutes,
		}, origin, destination, uc.now())
		if err != nil {
			return views.Lane{}, err
		}
		_, err = uc.Lanes.FindOpen(ctx, cmd.OriginNodeID, cmd.DestinationNodeID, cmd.Mode)
		duplicate, err := found(err)
		if err != nil {
			return views.Lane{}, err
		}
		if duplicate {
			return views.Lane{}, lane.ErrDuplicateLane
		}
		if err := uc.Lanes.Save(ctx, l); err != nil {
			return views.Lane{}, err
		}
		if err := uc.publish(ctx, events); err != nil {
			return views.Lane{}, err
		}
		return views.LaneFrom(l), nil
	})
}

// CloseLane closes an OPEN lane.
type CloseLane struct{ writer }

// NewCloseLane builds the use case.
func NewCloseLane(d Deps) *CloseLane { return &CloseLane{writer{d}} }

// Execute closes the lane and saves it with LaneClosed.
func (uc *CloseLane) Execute(ctx context.Context, id lane.ID) (views.Lane, error) {
	return transact(ctx, uc.UoW, func(ctx context.Context) (views.Lane, error) {
		l, err := uc.Lanes.Get(ctx, id)
		if err != nil {
			return views.Lane{}, err
		}
		events, err := l.Close(uc.now())
		if err != nil {
			return views.Lane{}, err
		}
		if err := uc.Lanes.Save(ctx, l); err != nil {
			return views.Lane{}, err
		}
		if err := uc.publish(ctx, events); err != nil {
			return views.Lane{}, err
		}
		return views.LaneFrom(l), nil
	})
}

// DeclareLaneCostCommand is the input of DeclareLaneCost.
type DeclareLaneCostCommand struct {
	LaneID           lane.ID
	CostPerUnitCents int64
}

// DeclareLaneCost records the planner's declared cost of moving one
// package-unit across an OPEN lane (ADR 0014).
type DeclareLaneCost struct{ writer }

// NewDeclareLaneCost builds the use case.
func NewDeclareLaneCost(d Deps) *DeclareLaneCost { return &DeclareLaneCost{writer{d}} }

// Execute declares the cost and saves the lane with LaneCostDeclared.
func (uc *DeclareLaneCost) Execute(ctx context.Context, cmd DeclareLaneCostCommand) (views.Lane, error) {
	return transact(ctx, uc.UoW, func(ctx context.Context) (views.Lane, error) {
		l, err := uc.Lanes.Get(ctx, cmd.LaneID)
		if err != nil {
			return views.Lane{}, err
		}
		events, err := l.DeclareCost(cmd.CostPerUnitCents, uc.now())
		if err != nil {
			return views.Lane{}, err
		}
		if err := uc.Lanes.Save(ctx, l); err != nil {
			return views.Lane{}, err
		}
		if err := uc.publish(ctx, events); err != nil {
			return views.Lane{}, err
		}
		return views.LaneFrom(l), nil
	})
}
