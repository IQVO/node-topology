package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

func TestDefineRegionDefinesARegion(t *testing.T) {
	e := newEnv(t)
	before := e.before()

	got, err := usecases.NewDefineRegion(e.deps).Execute(context.Background(), usecases.DefineRegionCommand{Code: "SP", Name: "Sao Paulo"})
	wantErr(t, err, nil)

	if got.RegionID != "region-1" || got.Code != "SP" || got.Name != "Sao Paulo" {
		t.Fatalf("view = %+v", got)
	}
	events := e.assertRaised(before, "RegionDefined")
	if events[0].SubjectID() != "region-1" || events[0].AggregateVersion() != 1 {
		t.Fatalf("event = %+v", events[0])
	}
	if _, err := e.store.Regions().FindByCode(context.Background(), "SP"); err != nil {
		t.Fatalf("region not stored: %v", err)
	}
}

func TestDefineRegionFailures(t *testing.T) {
	define := func(code string) func(*env) error {
		return func(e *env) error {
			_, err := usecases.NewDefineRegion(e.deps).Execute(context.Background(), usecases.DefineRegionCommand{Code: code, Name: "Name"})
			return err
		}
	}
	existing := func(e *env) { e.seedRegion("r-1", "SP") }
	with := func(set func(e *env)) func(e *env) { return func(e *env) { existing(e); set(e) } }
	runFailCases(t, []failCase{
		{"code taken", existing, define("SP"), region.ErrRegionCodeTaken},
		{"repository raises the taken code under a race", with(func(e *env) { e.regions.saveErrs = []error{region.ErrRegionCodeTaken} }), define("RJ"), region.ErrRegionCodeTaken},
		{"invalid code", noSetup, define("not a code"), shared.ErrValidation},
		{"code lookup fails", func(e *env) { e.regions.findErr = errBoom }, define("SP"), errBoom},
		{"save fails", func(e *env) { e.regions.saveErrs = []error{errBoom} }, define("SP"), errBoom},
		{"outbox fails after the save", func(e *env) { e.outbox.errs = []error{errBoom} }, define("SP"), errBoom},
		{"conflict twice", func(e *env) {
			e.regions.saveErrs = []error{ports.ErrConcurrentUpdate, ports.ErrConcurrentUpdate}
		}, define("SP"), ports.ErrConcurrentUpdate},
	})
}

func TestDefineRegionRetriesOnceOnAConcurrentUpdate(t *testing.T) {
	e := newEnv(t)
	e.regions.saveErrs = []error{ports.ErrConcurrentUpdate}
	before := e.before()

	_, err := usecases.NewDefineRegion(e.deps).Execute(context.Background(), usecases.DefineRegionCommand{Code: "SP", Name: "Sao Paulo"})
	wantErr(t, err, nil)

	if e.regions.saves != 2 {
		t.Fatalf("saves = %d, want 2", e.regions.saves)
	}
	e.assertRaised(before, "RegionDefined")
}
