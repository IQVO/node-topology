//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/adapters/outbound/clock"
	"github.com/claudioed/node-topology/internal/adapters/outbound/ids"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

var errBoom = errors.New("boom")

// rowCounts is the number of rows of every aggregate table plus the outbox.
type rowCounts struct{ regions, nodes, revisions, outbox int }

func countsOf(t *testing.T, pool *pgxpool.Pool) rowCounts {
	t.Helper()
	return rowCounts{
		regions: count(t, pool, "regions"), nodes: count(t, pool, "nodes"),
		revisions: count(t, pool, "node_profile_revisions"), outbox: count(t, pool, "outbox_events"),
	}
}

// definedEvents returns the events of defining the region.
func definedEvents(t *testing.T, id region.ID, code string) (*region.Region, []shared.Event) {
	t.Helper()
	r, events, err := region.Define(region.NewRegion{ID: id, Code: code, Name: "Region " + code}, repocontract.Now)
	noErr(t, err)
	return r, events
}

func TestFailedUnitLeavesNeitherAggregateNorOutboxRow(t *testing.T) {
	f, pool := newFixture(t)
	r, events := definedEvents(t, "region-1", "SP")

	err := f.UoW.Do(context.Background(), func(ctx context.Context) error {
		noErr(t, f.Regions.Save(ctx, r))
		noErr(t, f.Outbox.Append(ctx, events...))
		return errBoom
	})

	if !errors.Is(err, errBoom) {
		t.Fatalf("error = %v, want boom", err)
	}
	if got := countsOf(t, pool); got != (rowCounts{}) {
		t.Fatalf("rows after a failed unit = %+v, want none", got)
	}
}

func TestCommittedUnitStoresAggregateAndEventTogether(t *testing.T) {
	f, pool := newFixture(t)
	r, events := definedEvents(t, "region-1", "SP")

	noErr(t, f.UoW.Do(context.Background(), func(ctx context.Context) error {
		noErr(t, f.Regions.Save(ctx, r))
		return f.Outbox.Append(ctx, events...)
	}))

	if got := countsOf(t, pool); got != (rowCounts{regions: 1, outbox: 1}) {
		t.Fatalf("rows = %+v, want 1 region and 1 outbox row", got)
	}
}

func TestEncodeFailureWritesNoOutboxRowOfTheBatch(t *testing.T) {
	pool := newPool(t)
	f := fixtureOver(pool, testEncoder{failOn: "NodeActivated"})
	reg := repocontract.NewRegion(t, "region-1", "SP")
	n := repocontract.NewNode(t, reg, node.DeliveryStation, "node-1", "")
	_, registered, err := node.Register(node.Registration{
		ID: "node-2", Type: node.DeliveryStation, Operator: node.OperatorNetwork, RegionID: reg.ID(),
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	}, reg, repocontract.Now)
	noErr(t, err)
	activated, err := n.Activate(repocontract.Now)
	noErr(t, err)

	err = f.Outbox.Append(context.Background(), append(registered, activated...)...)

	if !errors.Is(err, errEncode) {
		t.Fatalf("error = %v, want the encoder's error", err)
	}
	if got := count(t, pool, "outbox_events"); got != 0 {
		t.Fatalf("%d outbox rows were written although an event of the batch failed to encode", got)
	}
}

func TestAppendWithoutEncoderIsAWiringError(t *testing.T) {
	pool := newPool(t)
	_, events := definedEvents(t, "region-1", "SP")
	err := postgres.NewOutboxRepo(pool, nil).Append(context.Background(), events...)
	if !errors.Is(err, postgres.ErrNilEncoder) {
		t.Fatalf("error = %v, want ErrNilEncoder", err)
	}
	noErr(t, postgres.NewOutboxRepo(pool, nil).Append(context.Background()))
}

func TestFailedWriteInsideAUnitDoesNotPoisonTheUnit(t *testing.T) {
	f, pool := newFixture(t)
	noErr(t, f.Regions.Save(context.Background(), repocontract.NewRegion(t, "region-1", "SP")))

	err := f.UoW.Do(context.Background(), func(ctx context.Context) error {
		if err := f.Regions.Save(ctx, repocontract.NewRegion(t, "region-2", "SP")); !errors.Is(err, region.ErrRegionCodeTaken) {
			t.Fatalf("duplicate code: error = %v", err)
		}
		// the unique violation rolled back to its savepoint only
		return f.Regions.Save(ctx, repocontract.NewRegion(t, "region-3", "RJ"))
	})

	noErr(t, err)
	if got := count(t, pool, "regions"); got != 2 {
		t.Fatalf("regions = %d, want 2 (the original and region-3)", got)
	}
}

func TestPartialNodeSaveInsideAFailedUnitIsRolledBack(t *testing.T) {
	f, pool := newFixture(t)
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	repocontract.SaveActive(t, f, reg, node.FulfillmentCenter, "node-1", "FC-1")
	before := countsOf(t, pool)

	err := f.UoW.Do(context.Background(), func(ctx context.Context) error {
		n, err := f.Nodes.Get(ctx, "node-1")
		noErr(t, err)
		second := repocontract.Declaration(2)
		second.HandlingTimeMinutes = 45
		_, err = n.DeclareProfile(second, repocontract.Now)
		noErr(t, err)
		noErr(t, f.Nodes.Save(ctx, n))
		return errBoom
	})

	if !errors.Is(err, errBoom) {
		t.Fatalf("error = %v", err)
	}
	if after := countsOf(t, pool); after != before {
		t.Fatalf("rows changed by a failed unit: before %+v, after %+v", before, after)
	}
	got, err := f.Nodes.Get(context.Background(), "node-1")
	noErr(t, err)
	if p, _ := got.Profile(); p.Revision != 1 || got.Version() != 3 {
		t.Fatalf("node is at revision %d version %d, want revision 1 version 3", p.Revision, got.Version())
	}
}

// useCases wires the application over the Postgres adapters.
func useCases(f repocontract.Fixture) usecases.Deps {
	return usecases.Deps{
		Nodes: f.Nodes, Lanes: f.Lanes, ServiceAreas: f.ServiceAreas, Regions: f.Regions,
		Outbox: f.Outbox, UoW: f.UoW, Clock: clock.System{}, IDs: ids.UUID{},
	}
}

func TestDefineRegionUseCaseCommitsRegionAndEventAtomically(t *testing.T) {
	f, pool := newFixture(t)
	define := usecases.NewDefineRegion(useCases(f))

	view, err := define.Execute(context.Background(), usecases.DefineRegionCommand{Code: "SP", Name: "Sao Paulo"})
	noErr(t, err)
	if view.Code != "SP" {
		t.Fatalf("view = %+v", view)
	}
	_, err = define.Execute(context.Background(), usecases.DefineRegionCommand{Code: "SP", Name: "Again"})

	if !errors.Is(err, region.ErrRegionCodeTaken) {
		t.Fatalf("error = %v, want ErrRegionCodeTaken", err)
	}
	if got := countsOf(t, pool); got != (rowCounts{regions: 1, outbox: 1}) {
		t.Fatalf("rows = %+v, want 1 region and 1 outbox row (the rejected command stored nothing)", got)
	}
	if _, err := f.Regions.Get(context.Background(), "nope"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("miss = %v", err)
	}
}
