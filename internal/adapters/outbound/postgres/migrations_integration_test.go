//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
)

// allTables is every table the migrations create, golang-migrate's own
// bookkeeping table excluded. There is deliberately no processed_events (inbox)
// table: node-topology consumes nothing.
var allTables = []string{
	"idempotency_keys", "lanes", "node_profile_revisions", "nodes", "outbox_events", "regions", "service_areas",
}

// latestMigration is the highest migration number; every ceiling below
// follows it (currently 0008, the lane emission).
const latestMigration = 8

// coreTables are the tables of migration 0001; 0002 adds the other two, 0003
// adds a column to node_profile_revisions only, and 0005, 0006 and 0008 add
// columns to lanes and service_areas.
var coreTables = []string{"lanes", "node_profile_revisions", "nodes", "regions", "service_areas"}

func tables(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name <> 'schema_migrations'
		ORDER BY table_name COLLATE "C"`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	return out
}

func wantTables(t *testing.T, pool *pgxpool.Pool, want []string) {
	t.Helper()
	if got := tables(t, pool); !slices.Equal(got, want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}
}

func migrator(t *testing.T, url string) *migrate.Migrate {
	t.Helper()
	m, err := postgres.NewMigrator(url)
	if err != nil {
		t.Fatalf("open migrator: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	return m
}

func wantVersion(t *testing.T, m *migrate.Migrate, want uint) {
	t.Helper()
	got, dirty, err := m.Version()
	if err != nil || dirty || got != want {
		t.Fatalf("version = %d dirty=%v err=%v, want %d clean", got, dirty, err, want)
	}
}

func TestMigrationsGoUpDownUp(t *testing.T) {
	url := emptyURL(t)
	pool := openPool(t, url)
	m := migrator(t, url)

	wantTables(t, pool, nil)
	if err := m.Up(); err != nil {
		t.Fatalf("up: %v", err)
	}
	wantVersion(t, m, latestMigration)
	wantTables(t, pool, allTables)

	if err := m.Down(); err != nil {
		t.Fatalf("down: %v", err)
	}
	wantTables(t, pool, nil)
	assertNoLeftovers(t, pool)

	if err := m.Up(); err != nil {
		t.Fatalf("up again: %v", err)
	}
	wantVersion(t, m, latestMigration)
	wantTables(t, pool, allTables)
}

// assertNoLeftovers fails when a migration down left a function or a type behind.
func assertNoLeftovers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var functions int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'public'`).Scan(&functions)
	if err != nil {
		t.Fatal(err)
	}
	if functions != 0 {
		t.Fatalf("%d functions survived the down migration", functions)
	}
}

func TestMigrationsStepDownOneRemovesOnlyTheInfrastructureTables(t *testing.T) {
	url := emptyURL(t)
	pool := openPool(t, url)
	m := migrator(t, url)
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(-(int(latestMigration) - 1)); err != nil {
		t.Fatalf("step down: %v", err)
	}
	wantVersion(t, m, 1)
	wantTables(t, pool, coreTables)
}

// inboundColumn reports whether node_profile_revisions has the nullable JSONB
// inbound column of migration 0003.
func inboundColumn(t *testing.T, pool *pgxpool.Pool) (present, nullable bool, dataType string) {
	t.Helper()
	var isNullable string
	err := pool.QueryRow(context.Background(), `
		SELECT is_nullable, data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'node_profile_revisions' AND column_name = 'inbound'`).Scan(&isNullable, &dataType)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, ""
	}
	if err != nil {
		t.Fatalf("inspect column: %v", err)
	}
	return true, isNullable == "YES", dataType
}

func TestMigration0003AddsAndDropsOnlyTheInboundColumn(t *testing.T) {
	url := emptyURL(t)
	pool := openPool(t, url)
	m := migrator(t, url)
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	if present, nullable, typ := inboundColumn(t, pool); !present || !nullable || typ != "jsonb" {
		t.Fatalf("inbound column = present %v nullable %v type %q, want a nullable jsonb column", present, nullable, typ)
	}
	if err := m.Steps(-(int(latestMigration) - 2)); err != nil {
		t.Fatalf("step down: %v", err)
	}
	wantVersion(t, m, 2)
	wantTables(t, pool, allTables)
	if present, _, _ := inboundColumn(t, pool); present {
		t.Fatal("the down migration left the inbound column behind")
	}
}

// serviceAreaModeColumns reports the delivery-mode columns migration 0006
// adds to service_areas (defaults rendered the way Postgres stores them,
// with the cast suffix).
func serviceAreaModeColumns(t *testing.T, pool *pgxpool.Pool) (mode, options bool) {
	t.Helper()
	for _, column := range []struct{ name, want string }{
		{"delivery_mode", "'NETWORK_ROUTE'"},
		{"carrier_options", "'[]'"},
	} {
		var def string
		err := pool.QueryRow(context.Background(), `
			SELECT column_default FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'service_areas' AND column_name = $1`, column.name).Scan(&def)
		if errors.Is(err, pgx.ErrNoRows) {
			return mode, options
		}
		if err != nil {
			t.Fatalf("inspect %s: %v", column.name, err)
		}
		if !strings.Contains(def, column.want) {
			t.Fatalf("%s default = %q, want it to contain %s", column.name, def, column.want)
		}
		if column.name == "delivery_mode" {
			mode = true
		} else {
			options = true
		}
	}
	return mode, options
}

// TestMigration0006AddsAndDropsOnlyTheServiceAreaColumns proves the delivery
// mode arrives as exactly two service_areas columns and leaves with them.
func TestMigration0006AddsAndDropsOnlyTheServiceAreaColumns(t *testing.T) {
	url := emptyURL(t)
	pool := openPool(t, url)
	m := migrator(t, url)
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	if mode, options := serviceAreaModeColumns(t, pool); !mode || !options {
		t.Fatal("the delivery-mode columns of migration 0006 are missing")
	}
	if err := m.Steps(-2); err != nil { // down through 0007, then 0006 itself
		t.Fatalf("step down: %v", err)
	}
	wantVersion(t, m, latestMigration-2)
	if mode, options := serviceAreaModeColumns(t, pool); mode || options {
		t.Fatal("the down migration left a delivery-mode column behind")
	}
}

// pickupPointColumn reports the serving-station column of migration 0007.
func pickupPointColumn(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_name = 'nodes' AND column_name = 'serving_station_node_id')`).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// TestMigration0007AddsAndDropsOnlyTheServingStationColumn proves the pickup
// point column arrives alone on nodes and leaves with it.
func TestMigration0007AddsAndDropsOnlyTheServingStationColumn(t *testing.T) {
	url := emptyURL(t)
	pool := openPool(t, url)
	m := migrator(t, url)
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	if !pickupPointColumn(t, pool) {
		t.Fatal("the serving-station column of migration 0007 is missing")
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("step down: %v", err)
	}
	wantVersion(t, m, latestMigration-1)
	if pickupPointColumn(t, pool) {
		t.Fatal("the down migration left the serving-station column behind")
	}
}

// laneEmissionColumn reports whether lanes carries the declared emission
// column of migration 0008.
func laneEmissionColumn(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_name = 'lanes' AND column_name = 'emission_per_unit_g')`).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// TestMigration0008AddsAndDropsOnlyTheLaneEmissionColumn proves the emission
// column arrives alone on lanes and leaves with it.
func TestMigration0008AddsAndDropsOnlyTheLaneEmissionColumn(t *testing.T) {
	url := emptyURL(t)
	pool := openPool(t, url)
	m := migrator(t, url)
	if err := m.Up(); err != nil {
		t.Fatalf("up: %v", err)
	}
	if !laneEmissionColumn(t, pool) {
		t.Fatal("the emission column of migration 0008 is missing")
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("step down: %v", err)
	}
	wantVersion(t, m, latestMigration-1)
	if laneEmissionColumn(t, pool) {
		t.Fatal("the down migration left the emission column behind")
	}
}

func TestRunMigrationsIsIdempotent(t *testing.T) {
	url := emptyURL(t)
	pool := openPool(t, url)
	for i := 0; i < 2; i++ {
		if err := postgres.RunMigrations(url); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	wantTables(t, pool, allTables)
}

func TestRunMigrationsReportsAnUnreachableDatabase(t *testing.T) {
	err := postgres.RunMigrations("postgres://nobody:nothing@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("migrating an unreachable database succeeded")
	}
}
