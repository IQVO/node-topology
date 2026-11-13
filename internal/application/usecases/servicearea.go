package usecases

import (
	"context"

	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// CarrierOptionCommand is one declared carrier option of an assignment: the
// carrier, the network node packages are handed over at, and the declared
// per-package cost and transit (ADR 0016).
type CarrierOptionCommand struct {
	CarrierID           string
	HandoffNodeID       node.ID
	CostPerPackageCents int64
	TransitMinutes      int
}

// AssignServiceAreaCommand is the input of AssignServiceArea. An empty
// DeliveryMode with no carriers is the NETWORK_ROUTE of today.
type AssignServiceAreaCommand struct {
	PostalPrefix          servicearea.PostalPrefix
	DeliveryStationNodeID node.ID
	AssignmentVersion     int
	DeliveryMode          servicearea.DeliveryMode
	Carriers              []CarrierOptionCommand
}

// AssignServiceArea assigns a postal prefix to a delivery station.
type AssignServiceArea struct{ writer }

// NewAssignServiceArea builds the use case.
func NewAssignServiceArea(d Deps) *AssignServiceArea { return &AssignServiceArea{writer{d}} }

// Execute loads the station (unknown is ports.ErrNotFound) and the station's
// region (a missing one reaches the domain as nil, region.ErrUnknownRegion),
// loads the distinct handoff nodes of the declared carrier options, then
// creates the service area or, when the prefix is already assigned,
// reassigns it under the new version, and saves it with ServiceAreaAssigned.
func (uc *AssignServiceArea) Execute(ctx context.Context, cmd AssignServiceAreaCommand) (views.ServiceArea, error) {
	return transact(ctx, uc.UoW, func(ctx context.Context) (views.ServiceArea, error) {
		station, err := uc.Nodes.Get(ctx, cmd.DeliveryStationNodeID)
		if err != nil {
			return views.ServiceArea{}, err
		}
		reg, err := optionalRegion(ctx, uc.Regions, station.RegionID())
		if err != nil {
			return views.ServiceArea{}, err
		}
		carrier, err := uc.carrierDelivery(ctx, cmd)
		if err != nil {
			return views.ServiceArea{}, err
		}
		area, events, err := uc.assign(ctx, cmd, station, reg, carrier)
		if err != nil {
			return views.ServiceArea{}, err
		}
		if err := uc.ServiceAreas.Save(ctx, area); err != nil {
			return views.ServiceArea{}, err
		}
		if err := uc.publish(ctx, events); err != nil {
			return views.ServiceArea{}, err
		}
		return views.ServiceAreaFrom(area), nil
	})
}

// carrierDelivery builds the domain's carrier half of the assignment and
// loads each distinct handoff node (unknown is ports.ErrNotFound).
func (uc *AssignServiceArea) carrierDelivery(
	ctx context.Context, cmd AssignServiceAreaCommand,
) (servicearea.CarrierDelivery, error) {
	out := servicearea.CarrierDelivery{Mode: cmd.DeliveryMode, Carriers: make([]servicearea.CarrierOption, len(cmd.Carriers))}
	loaded := make(map[node.ID]*node.Node, len(cmd.Carriers))
	for i, o := range cmd.Carriers {
		out.Carriers[i] = servicearea.CarrierOption{
			CarrierID: o.CarrierID, HandoffNodeID: o.HandoffNodeID,
			CostPerPackageCents: o.CostPerPackageCents, TransitMinutes: o.TransitMinutes,
		}
		if _, ok := loaded[o.HandoffNodeID]; ok {
			continue
		}
		h, err := uc.Nodes.Get(ctx, o.HandoffNodeID)
		if err != nil {
			return servicearea.CarrierDelivery{}, err
		}
		loaded[o.HandoffNodeID] = h
	}
	for _, h := range loaded {
		out.Handoffs = append(out.Handoffs, h)
	}
	return out, nil
}

// assign creates the first assignment of the prefix or reassigns it.
func (uc *AssignServiceArea) assign(
	ctx context.Context, cmd AssignServiceAreaCommand, station *node.Node, reg *region.Region, carrier servicearea.CarrierDelivery,
) (*servicearea.ServiceArea, []shared.Event, error) {
	area, err := uc.ServiceAreas.Get(ctx, cmd.PostalPrefix)
	assigned, err := found(err)
	if err != nil {
		return nil, nil, err
	}
	if !assigned {
		return servicearea.Assign(cmd.PostalPrefix, cmd.AssignmentVersion, station, reg, carrier, uc.now())
	}
	events, err := area.Reassign(cmd.AssignmentVersion, station, reg, carrier, uc.now())
	return area, events, err
}
