package repocontract

import (
	"context"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/region"
)

var regionCases = []named{
	{"save then get round-trips the snapshot", regionRoundTrip},
	{"get of an unknown id is ErrNotFound", regionGetMiss},
	{"find by code hits and misses", regionFindByCode},
	{"a second region with the same code is ErrRegionCodeTaken", regionCodeTaken},
	{"creating an id that exists is ErrConcurrentUpdate", regionDuplicateID},
}

func regionRoundTrip(t *testing.T, f Fixture) {
	ctx := context.Background()
	r := NewRegion(t, "region-1", "SP")
	noErr(t, f.Regions.Save(ctx, r))
	got, err := f.Regions.Get(ctx, "region-1")
	noErr(t, err)
	if !reflect.DeepEqual(got.Snapshot(), r.Snapshot()) {
		t.Fatalf("snapshot = %+v, want %+v", got.Snapshot(), r.Snapshot())
	}
}

func regionGetMiss(t *testing.T, f Fixture) {
	_, err := f.Regions.Get(context.Background(), "nope")
	wantErr(t, err, ports.ErrNotFound)
}

func regionFindByCode(t *testing.T, f Fixture) {
	ctx := context.Background()
	noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-1", "SP")))
	got, err := f.Regions.FindByCode(ctx, "SP")
	noErr(t, err)
	if got.ID() != "region-1" {
		t.Fatalf("id = %s, want region-1", got.ID())
	}
	_, err = f.Regions.FindByCode(ctx, "RJ")
	wantErr(t, err, ports.ErrNotFound)
}

func regionCodeTaken(t *testing.T, f Fixture) {
	ctx := context.Background()
	noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-1", "SP")))
	wantErr(t, f.Regions.Save(ctx, NewRegion(t, "region-2", "SP")), region.ErrRegionCodeTaken)
	if _, err := f.Regions.Get(ctx, "region-2"); err == nil {
		t.Fatal("the rejected region was stored")
	}
	noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-3", "RJ")))
}

func regionDuplicateID(t *testing.T, f Fixture) {
	ctx := context.Background()
	noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-1", "SP")))
	wantErr(t, f.Regions.Save(ctx, NewRegion(t, "region-1", "RJ")), ports.ErrConcurrentUpdate)
}
