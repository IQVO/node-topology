package repocontract

import (
	"context"
	"errors"
	"testing"

	"github.com/claudioed/node-topology/internal/domain/region"
)

var unitOfWorkCases = []named{
	{"a returned nil commits every save", uowCommit},
	{"a returned error rolls every save back and is returned unchanged", uowRollback},
	{"a panic rolls back and propagates", uowPanic},
	{"a nested Do joins the outer unit", uowNested},
	{"the outbox commits and rolls back with the aggregates", uowOutbox},
}

var errBoom = errors.New("boom")

// twoRegions saves two regions through the repository on ctx.
func twoRegions(t *testing.T, f Fixture, ctx context.Context) error {
	t.Helper()
	if err := f.Regions.Save(ctx, NewRegion(t, "region-1", "SP")); err != nil {
		return err
	}
	return f.Regions.Save(ctx, NewRegion(t, "region-2", "RJ"))
}

func regionStored(f Fixture, id region.ID) bool {
	_, err := f.Regions.Get(context.Background(), id)
	return err == nil
}

func uowCommit(t *testing.T, f Fixture) {
	noErr(t, f.UoW.Do(context.Background(), func(ctx context.Context) error { return twoRegions(t, f, ctx) }))
	if !regionStored(f, "region-1") || !regionStored(f, "region-2") {
		t.Fatal("a committed unit lost a save")
	}
}

func uowRollback(t *testing.T, f Fixture) {
	err := f.UoW.Do(context.Background(), func(ctx context.Context) error {
		noErr(t, twoRegions(t, f, ctx))
		return errBoom
	})
	wantErr(t, err, errBoom)
	if regionStored(f, "region-1") || regionStored(f, "region-2") {
		t.Fatal("a failed unit left a save behind")
	}
}

func uowPanic(t *testing.T, f Fixture) {
	defer func() {
		if recover() == nil {
			t.Fatal("the panic was swallowed")
		}
		if regionStored(f, "region-1") {
			t.Fatal("a panicking unit left a save behind")
		}
	}()
	_ = f.UoW.Do(context.Background(), func(ctx context.Context) error {
		noErr(t, f.Regions.Save(ctx, NewRegion(t, "region-1", "SP")))
		panic("kaboom")
	})
}

func uowNested(t *testing.T, f Fixture) {
	err := f.UoW.Do(context.Background(), func(ctx context.Context) error {
		noErr(t, f.UoW.Do(ctx, func(ctx context.Context) error {
			return f.Regions.Save(ctx, NewRegion(t, "region-1", "SP"))
		}))
		return errBoom
	})
	wantErr(t, err, errBoom)
	if regionStored(f, "region-1") {
		t.Fatal("the inner unit committed on its own")
	}
}

func uowOutbox(t *testing.T, f Fixture) {
	if f.OutboxCount == nil {
		t.Skip("the fixture cannot count its outbox")
	}
	ctx := context.Background()
	r := NewRegion(t, "region-1", "SP")
	_, events, err := region.Define(region.NewRegion{ID: "region-9", Code: "XX", Name: "x"}, Now)
	noErr(t, err)
	failed := f.UoW.Do(ctx, func(ctx context.Context) error {
		noErr(t, f.Regions.Save(ctx, r))
		noErr(t, f.Outbox.Append(ctx, events...))
		return errBoom
	})
	wantErr(t, failed, errBoom)
	assertOutbox(t, f, 0)
	noErr(t, f.UoW.Do(ctx, func(ctx context.Context) error {
		noErr(t, f.Regions.Save(ctx, r))
		return f.Outbox.Append(ctx, events...)
	}))
	assertOutbox(t, f, len(events))
}

func assertOutbox(t *testing.T, f Fixture, want int) {
	t.Helper()
	got, err := f.OutboxCount(context.Background())
	noErr(t, err)
	if got != want {
		t.Fatalf("outbox holds %d events, want %d", got, want)
	}
}
