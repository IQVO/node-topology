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
	{"list is empty, never nil, on an empty database", regionListEmpty},
	{"list returns every region ordered by id", regionListOrdered},
	{"list skips a rejected region", regionListSkipsRejected},
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

func regionListEmpty(t *testing.T, f Fixture) {
	got, err := f.Regions.List(context.Background())
	noErr(t, err)
	if got == nil || len(got) != 0 {
		t.Fatalf("list = %#v, want a non-nil empty list", got)
	}
}

func regionListOrdered(t *testing.T, f Fixture) {
	ctx := context.Background()
	noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-b", "RJ")))
	noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-c", "MG")))
	noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-a", "SP")))
	got, err := f.Regions.List(ctx)
	noErr(t, err)
	ids := []region.ID{}
	for _, r := range got {
		ids = append(ids, r.ID())
	}
	if want := []region.ID{"region-a", "region-b", "region-c"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if got[0].Code() != "SP" || got[0].Name() != "Region SP" {
		t.Fatalf("first region = %s %q", got[0].Code(), got[0].Name())
	}
}

func regionListSkipsRejected(t *testing.T, f Fixture) {
	ctx := context.Background()
	noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-1", "SP")))
	wantErr(t, f.Regions.Save(ctx, NewRegion(t, "region-2", "SP")), region.ErrRegionCodeTaken)
	got, err := f.Regions.List(ctx)
	noErr(t, err)
	if len(got) != 1 || got[0].ID() != "region-1" {
		t.Fatalf("list holds %d regions, want only region-1", len(got))
	}
}
