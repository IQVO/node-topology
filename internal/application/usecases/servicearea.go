package usecases

import (
	"context"

	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// AssignServiceAreaCommand is the input of AssignServiceArea.
type AssignServiceAreaCommand struct {
	PostalPrefix          servicearea.PostalPrefix
	DeliveryStationNodeID node.ID
	AssignmentVersion     int
}

// AssignServiceArea assigns a postal prefix to a delivery station.
type AssignServiceArea struct{ writer }

// NewAssignServiceArea builds the use case.
func NewAssignServiceArea(d Deps) *AssignServiceArea { return &AssignServiceArea{writer{d}} }

// Execute loads the station (unknown is ports.ErrNotFound) and the station's
// region (a missing one reaches the domain as nil, region.ErrUnknownRegion),
// then creates the service area or, when the prefix is already assigned,
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
		area, events, err := uc.assign(ctx, cmd, station, reg)
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

// assign creates the first assignment of the prefix or reassigns it.
func (uc *AssignServiceArea) assign(
	ctx context.Context, cmd AssignServiceAreaCommand, station *node.Node, reg *region.Region,
) (*servicearea.ServiceArea, []shared.Event, error) {
	area, err := uc.ServiceAreas.Get(ctx, cmd.PostalPrefix)
	assigned, err := found(err)
	if err != nil {
		return nil, nil, err
	}
	if !assigned {
		return servicearea.Assign(cmd.PostalPrefix, cmd.AssignmentVersion, station, reg, uc.now())
	}
	events, err := area.Reassign(cmd.AssignmentVersion, station, reg, uc.now())
	return area, events, err
}
