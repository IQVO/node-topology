//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// revisionRows returns every stored revision row as JSON, oldest first.
func revisionRows(t *testing.T, pool *pgxpool.Pool, id string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT to_jsonb(r)::text FROM node_profile_revisions r WHERE node_id = $1 ORDER BY revision`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var doc string
		if err := rows.Scan(&doc); err != nil {
			t.Fatal(err)
		}
		out = append(out, doc)
	}
	return out
}

// declareRevision declares revision n (with n*15 minutes of handling) on the
// stored node and saves it.
func declareRevision(t *testing.T, f repocontract.Fixture, id node.ID, n int) {
	t.Helper()
	ctx := context.Background()
	stored, err := f.Nodes.Get(ctx, id)
	noErr(t, err)
	d := repocontract.Declaration(n)
	d.HandlingTimeMinutes = n * 15
	_, err = stored.DeclareProfile(d, repocontract.Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, stored))
}

func TestSavingANewRevisionLeavesTheOldRowsUntouched(t *testing.T) {
	f, pool := newFixture(t)
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	repocontract.SaveActive(t, f, reg, node.FulfillmentCenter, "node-1", "FC-1")
	first := revisionRows(t, pool, "node-1")

	declareRevision(t, f, "node-1", 2)
	declareRevision(t, f, "node-1", 3)

	rows := revisionRows(t, pool, "node-1")
	if len(rows) != 3 || rows[0] != first[0] {
		t.Fatalf("revision rows = %d, first row changed = %v", len(rows), len(rows) > 0 && rows[0] != first[0])
	}
	history, err := f.Nodes.ProfileRevisions(context.Background(), "node-1")
	noErr(t, err)
	if len(history) != 3 || history[0].HandlingTimeMinutes != 30 || history[1].HandlingTimeMinutes != 30 || history[2].HandlingTimeMinutes != 45 {
		t.Fatalf("history = %+v", history)
	}
	var current int
	noErr(t, pool.QueryRow(context.Background(), `SELECT profile_revision FROM nodes WHERE id = 'node-1'`).Scan(&current))
	if current != 3 {
		t.Fatalf("nodes.profile_revision = %d, want 3", current)
	}
}

func TestProfileRevisionRowsRefuseUpdateAndDelete(t *testing.T) {
	f, pool := newFixture(t)
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	repocontract.SaveActive(t, f, reg, node.FulfillmentCenter, "node-1", "FC-1")
	before := revisionRows(t, pool, "node-1")

	updateErr := exec(t, pool, `UPDATE node_profile_revisions SET handling_time_minutes = 1 WHERE node_id = 'node-1'`)
	deleteErr := exec(t, pool, `DELETE FROM node_profile_revisions WHERE node_id = 'node-1'`)

	if updateErr == nil || deleteErr == nil {
		t.Fatalf("a stored revision was changed: update err=%v delete err=%v", updateErr, deleteErr)
	}
	if after := revisionRows(t, pool, "node-1"); !reflect.DeepEqual(after, before) {
		t.Fatalf("revision rows changed: %v -> %v", before, after)
	}
}

func TestNodeCannotPointAtAMissingRevision(t *testing.T) {
	f, pool := newFixture(t)
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	repocontract.SaveActive(t, f, reg, node.FulfillmentCenter, "node-1", "FC-1")

	if err := exec(t, pool, `UPDATE nodes SET profile_revision = 9 WHERE id = 'node-1'`); err == nil {
		t.Fatal("a node was pointed at a revision that does not exist")
	}
}

func TestProfileDocumentRoundTripsNilAndEmptyCollections(t *testing.T) {
	f, pool := newFixture(t)
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	repocontract.SaveActive(t, f, reg, node.FulfillmentCenter, "node-1", "FC-1")
	declareRevision(t, f, "node-1", 2)

	var hours, holidays, cutoffs string
	err := pool.QueryRow(context.Background(), `
		SELECT operating_hours::text, holidays::text, cutoffs::text FROM node_profile_revisions
		WHERE node_id = 'node-1' AND revision = 2`).Scan(&hours, &holidays, &cutoffs)
	noErr(t, err)
	if holidays != "[]" {
		t.Fatalf("holidays = %s, want []", holidays)
	}
	var doc map[string]map[string]string
	noErr(t, json.Unmarshal([]byte(hours), &doc))
	if doc["monday"]["start"] != "06:00" || doc["monday"]["end"] != "22:00" {
		t.Fatalf("operating hours = %s", hours)
	}
}

func TestListOrdersIDsByteWise(t *testing.T) {
	f, _ := newFixture(t)
	ctx := context.Background()
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	for _, id := range []node.ID{"node-b", "node-B", "node-a", "node-A", "node-1"} {
		noErr(t, f.Nodes.Save(ctx, repocontract.NewNode(t, reg, node.DeliveryStation, id, "")))
	}

	got, err := f.Nodes.List(ctx, repository.NodeFilter{})
	noErr(t, err)

	ids := []node.ID{}
	for _, n := range got {
		ids = append(ids, n.ID())
	}
	if want := []node.ID{"node-1", "node-A", "node-B", "node-a", "node-b"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want the byte-wise order %v (the in-memory doubles sort the same way)", ids, want)
	}
}

func TestRegionListOrdersIDsByteWise(t *testing.T) {
	f, _ := newFixture(t)
	ctx := context.Background()
	noErr(t, f.Regions.Save(ctx, repocontract.NewRegion(t, "region-b", "BB")))
	noErr(t, f.Regions.Save(ctx, repocontract.NewRegion(t, "region-B", "AA")))

	got, err := f.Regions.List(ctx)
	noErr(t, err)
	if len(got) != 2 || got[0].ID() != region.ID("region-B") {
		t.Fatalf("regions = %v, want region-B first", got)
	}
}

func TestACorruptRowIsNeverRestoredIntoAnAggregate(t *testing.T) {
	f, pool := newFixture(t)
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	// An ACTIVE FULFILLMENT_CENTER with no profile passes every column check but
	// breaks the aggregate's invariant.
	err := exec(t, pool, `
		INSERT INTO nodes (id, node_type, subtype, operator, vendor_id, vendor_node_code, region_id, timezone,
			postal_code, country_code, status, activated_at, version)
		VALUES ('node-bad', 'FULFILLMENT_CENTER', NULL, 'VENDOR', 'vendor-1', 'FC-BAD', $1, 'America/Sao_Paulo',
			'01310', 'BR', 'ACTIVE', now(), 1)`, string(reg.ID()))
	noErr(t, err)

	_, getErr := f.Nodes.Get(context.Background(), "node-bad")
	_, listErr := f.Nodes.List(context.Background(), repository.NodeFilter{})

	for name, e := range map[string]error{"get": getErr, "list": listErr} {
		if !errors.Is(e, shared.ErrInvalidSnapshot) && !errors.Is(e, node.ErrIncompleteOperationalProfile) {
			t.Fatalf("%s: error = %v, want a corrupt-row error", name, e)
		}
	}
}

func TestVendorCodesAreOnlyConstrainedForVendorNodes(t *testing.T) {
	f, _ := newFixture(t)
	ctx := context.Background()
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	for _, id := range []node.ID{"node-1", "node-2", "node-3"} {
		noErr(t, f.Nodes.Save(ctx, repocontract.NewNode(t, reg, node.DeliveryStation, id, "")))
	}
}
