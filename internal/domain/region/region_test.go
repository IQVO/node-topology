package region

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func valid() NewRegion { return NewRegion{ID: "region-se", Code: "SE", Name: "Southeast"} }

func TestDefineHappyPath(t *testing.T) {
	brt := time.FixedZone("BRT", -3*3600)
	r, events, err := Define(valid(), t0.In(brt))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID() != "region-se" || r.Code() != "SE" || r.Name() != "Southeast" || r.Version() != 1 {
		t.Fatalf("unexpected region: %+v", r.Snapshot())
	}
	if !r.DefinedAt().Equal(t0) || r.DefinedAt().Location() != time.UTC {
		t.Fatalf("definedAt must be UTC: %v", r.DefinedAt())
	}
	if len(events) != 1 {
		t.Fatalf("want one event, got %d", len(events))
	}
	ev, ok := events[0].(RegionDefined)
	if !ok {
		t.Fatalf("want RegionDefined, got %T", events[0])
	}
	want := RegionDefined{Header: shared.Header{Version: 1, At: t0}, RegionID: "region-se", Code: "SE", Name: "Southeast"}
	if ev != want {
		t.Fatalf("event = %+v, want %+v", ev, want)
	}
	assertEventAccessors(t, ev)
}

func assertEventAccessors(t *testing.T, ev RegionDefined) {
	t.Helper()
	if ev.EventName() != "RegionDefined" || ev.Entity() != "region" || ev.SubjectID() != "region-se" ||
		ev.AggregateVersion() != 1 || !ev.OccurredAt().Equal(t0) {
		t.Fatalf("event accessors wrong: %+v", ev)
	}
}

func TestDefineRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*NewRegion)
	}{
		{"empty id", func(n *NewRegion) { n.ID = "" }},
		{"id with space", func(n *NewRegion) { n.ID = "region se" }},
		{"empty code", func(n *NewRegion) { n.Code = "" }},
		{"lowercase code", func(n *NewRegion) { n.Code = "se" }},
		{"code with space", func(n *NewRegion) { n.Code = "S E" }},
		{"code too long", func(n *NewRegion) { n.Code = strings.Repeat("A", MaxCode+1) }},
		{"empty name", func(n *NewRegion) { n.Name = "" }},
		{"blank name", func(n *NewRegion) { n.Name = "   " }},
		{"untrimmed name", func(n *NewRegion) { n.Name = " Southeast" }},
		{"name too long", func(n *NewRegion) { n.Name = strings.Repeat("n", MaxName+1) }},
		{"name with control char", func(n *NewRegion) { n.Name = "South\neast" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := valid()
			tc.mut(&in)
			r, events, err := Define(in, t0)
			if !errors.Is(err, shared.ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
			if r != nil || events != nil {
				t.Fatalf("nothing may be returned on error: %v %v", r, events)
			}
		})
	}
}

func TestDefineBoundaryValuesAccepted(t *testing.T) {
	in := NewRegion{ID: "r", Code: strings.Repeat("A", MaxCode), Name: strings.Repeat("é", MaxName)}
	if _, _, err := Define(in, t0); err != nil {
		t.Fatalf("limits are inclusive: %v", err)
	}
	for _, code := range []string{"A", "Z", "0", "9", "_", "-", "A_1-Z9"} {
		if _, _, err := Define(NewRegion{ID: "r", Code: code, Name: "n"}, t0); err != nil {
			t.Errorf("code %q must be accepted: %v", code, err)
		}
	}
	for _, code := range []string{"@", "[", "`", "{", "/", ":"} { // neighbours of the accepted ranges
		if _, _, err := Define(NewRegion{ID: "r", Code: code, Name: "n"}, t0); err == nil {
			t.Errorf("code %q must be refused", code)
		}
	}
}

func TestDefineRejectsZeroTime(t *testing.T) {
	if _, _, err := Define(valid(), time.Time{}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	r, _, err := Define(valid(), t0)
	if err != nil {
		t.Fatal(err)
	}
	snap := r.Snapshot()
	want := Snapshot{ID: "region-se", Code: "SE", Name: "Southeast", DefinedAt: t0, Version: 1}
	if snap != want {
		t.Fatalf("snapshot = %+v, want %+v", snap, want)
	}
	back, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if back.Snapshot() != snap {
		t.Fatalf("round trip changed the region: %+v", back.Snapshot())
	}
}

func TestRestoreRejectsCorruptRows(t *testing.T) {
	good := Snapshot{ID: "region-se", Code: "SE", Name: "Southeast", DefinedAt: t0, Version: 3}
	if r, err := Restore(good); err != nil || r.Version() != 3 {
		t.Fatalf("a valid row restores: %v", err)
	}
	tests := []struct {
		name string
		mut  func(*Snapshot)
	}{
		{"bad id", func(s *Snapshot) { s.ID = "" }},
		{"bad code", func(s *Snapshot) { s.Code = "x y" }},
		{"bad name", func(s *Snapshot) { s.Name = "" }},
		{"zero definedAt", func(s *Snapshot) { s.DefinedAt = time.Time{} }},
		{"version zero", func(s *Snapshot) { s.Version = 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := good
			tc.mut(&s)
			if _, err := Restore(s); !errors.Is(err, shared.ErrInvalidSnapshot) {
				t.Fatalf("want ErrInvalidSnapshot, got %v", err)
			}
		})
	}
	if errors.Is(ErrRegionCodeTaken, shared.ErrValidation) || ErrRegionCodeTaken.Error() == "" {
		t.Fatal("ErrRegionCodeTaken must be its own sentinel")
	}
}
