package usecases

import (
	"context"

	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// DefineRegionCommand is the input of DefineRegion.
type DefineRegionCommand struct {
	Code string
	Name string
}

// DefineRegion defines a region.
type DefineRegion struct{ writer }

// NewDefineRegion builds the use case.
func NewDefineRegion(d Deps) *DefineRegion { return &DefineRegion{writer{d}} }

// Execute mints the region id, defines the region, checks that its code is
// free (region.ErrRegionCodeTaken) and saves it with RegionDefined.
func (uc *DefineRegion) Execute(ctx context.Context, cmd DefineRegionCommand) (views.Region, error) {
	return transact(ctx, uc.UoW, func(ctx context.Context) (views.Region, error) {
		r, events, err := region.Define(region.NewRegion{
			ID: uc.IDs.NewRegionID(), Code: cmd.Code, Name: cmd.Name,
		}, uc.now())
		if err != nil {
			return views.Region{}, err
		}
		_, err = uc.Regions.FindByCode(ctx, cmd.Code)
		taken, err := found(err)
		if err != nil {
			return views.Region{}, err
		}
		if taken {
			return views.Region{}, region.ErrRegionCodeTaken
		}
		if err := uc.Regions.Save(ctx, r); err != nil {
			return views.Region{}, err
		}
		if err := uc.publish(ctx, events); err != nil {
			return views.Region{}, err
		}
		return views.RegionFrom(r), nil
	})
}
