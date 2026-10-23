package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/domain/node"
)

// rowDeclaration is one valid declaration, the same shape the domain's own
// tests use, so the round trip has something realistic to carry.
func rowDeclaration(rev int) node.ProfileDeclaration {
	return node.ProfileDeclaration{
		Revision:            rev,
		Timezone:            "America/Sao_Paulo",
		OperatingHours:      node.OperatingHours{node.Monday: {Start: "06:00", End: "22:00"}},
		Holidays:            []string{"2026-12-25"},
		HandlingTimeMinutes: 240,
		Cutoffs: []node.CutoffSlot{
			{LocalTime: "11:00"},
			{LocalTime: "18:00", Days: []node.Weekday{node.Monday}, MaxOrders: 1200},
		},
		ThroughputCap: node.ThroughputCap{Value: 400, TimeUnit: node.TimeUnitHour, Unit: node.UnitOrder},
		DeclaredBy:    node.DeclaredByVendor,
	}
}

func rowFor(d node.ProfileDeclaration, at time.Time) (*profileRow, error) {
	hours, err := encodeHours(d.OperatingHours)
	if err != nil {
		return nil, err
	}
	cutoffs, err := encodeCutoffs(d.Cutoffs)
	if err != nil {
		return nil, err
	}
	holidays, err := json.Marshal(d.Holidays)
	if err != nil {
		return nil, err
	}
	rev, handling, value := d.Revision, d.HandlingTimeMinutes, d.ThroughputCap.Value
	unitOf, unit := d.ThroughputCap.TimeUnit, d.ThroughputCap.Unit
	tz, by := d.Timezone, string(d.DeclaredBy)
	declaredAt := at
	return &profileRow{
		Revision: &rev, Timezone: &tz, OperatingHours: hours, Holidays: holidays, HandlingTime: &handling,
		Cutoffs: cutoffs, ThroughputValue: &value, ThroughputUnitOf: (*string)(&unitOf), ThroughputUnit: &unit,
		SortDwell: d.SortDwellMinutes, DeclaredBy: &by, DeclaredAt: &declaredAt,
	}, nil
}

func TestProfileRowRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	row, err := rowFor(rowDeclaration(2), at)
	if err != nil {
		t.Fatalf("build row: %v", err)
	}
	got, ok, err := row.profile()
	if err != nil {
		t.Fatalf("profile() error: %v", err)
	}
	if !ok {
		t.Fatal("profile() ok = false, want true")
	}
	if got.Revision != 2 || got.Timezone != "America/Sao_Paulo" || got.HandlingTimeMinutes != 240 {
		t.Fatalf("profile() = %+v, want the stored scalar fields", got.ProfileDeclaration)
	}
	if len(got.OperatingHours) != 1 || got.OperatingHours[node.Monday] != (node.DayHours{Start: "06:00", End: "22:00"}) {
		t.Fatalf("profile() operating hours = %v, want monday 06:00-22:00", got.OperatingHours)
	}
	if len(got.Cutoffs) != 2 || got.Cutoffs[1].LocalTime != "18:00" || got.Cutoffs[1].MaxOrders != 1200 {
		t.Fatalf("profile() cutoffs = %v, want both slots with their fields", got.Cutoffs)
	}
	if got.ThroughputCap != (node.ThroughputCap{Value: 400, TimeUnit: node.TimeUnitHour, Unit: node.UnitOrder}) {
		t.Fatalf("profile() throughput = %v, want the stored cap", got.ThroughputCap)
	}
	if !got.DeclaredAt.Equal(at) {
		t.Fatalf("profile() declaredAt = %v, want %v", got.DeclaredAt, at)
	}
}

func TestProfileRowNullJoinSide(t *testing.T) {
	row := &profileRow{} // the all-NULL side of a LEFT JOIN
	_, ok, err := row.profile()
	if err != nil {
		t.Fatalf("profile() error: %v", err)
	}
	if ok {
		t.Fatal("profile() ok = true for an all-NULL row, want false")
	}
}

func TestProfileRowRejectsCorruptRows(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*profileRow)
	}{
		{"operating hours are not JSON", func(r *profileRow) { r.OperatingHours = []byte("not json") }},
		{"operating hours are the wrong shape", func(r *profileRow) { r.OperatingHours = []byte(`[1,2]`) }},
		{"cutoffs are not JSON", func(r *profileRow) { r.Cutoffs = []byte("not json") }},
		{"holidays are not JSON", func(r *profileRow) { r.Holidays = []byte("not json") }},
		{"the timezone is not a shaped IANA name", func(r *profileRow) { tz := "123/Zone"; r.Timezone = &tz }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row, err := rowFor(rowDeclaration(1), at)
			if err != nil {
				t.Fatalf("build row: %v", err)
			}
			tc.mutate(row)
			if _, _, err := row.profile(); err == nil {
				t.Fatal("profile() accepted a corrupt row, want an error")
			}
		})
	}
}

func TestProfileRowDestMatchesColumns(t *testing.T) {
	row := &profileRow{}
	if got := len(row.dest()); got != 12 {
		t.Fatalf("dest() has %d targets, want 12 (one per column of profileColumns)", got)
	}
}

func TestDecodeHoursDoc(t *testing.T) {
	t.Run("null is no hours", func(t *testing.T) {
		hours, err := decodeHours([]byte("null"))
		if err != nil {
			t.Fatalf("decodeHours(null): %v", err)
		}
		if hours != nil {
			t.Fatalf("decodeHours(null) = %v, want nil", hours)
		}
	})
	t.Run("an empty object is empty hours", func(t *testing.T) {
		hours, err := decodeHours([]byte("{}"))
		if err != nil {
			t.Fatalf("decodeHours({}): %v", err)
		}
		if len(hours) != 0 {
			t.Fatalf("decodeHours({}) = %v, want empty", hours)
		}
	})
	t.Run("corrupt JSON is an error", func(t *testing.T) {
		if _, err := decodeHours([]byte("{")); err == nil {
			t.Fatal("decodeHours of corrupt JSON = no error")
		}
	})
}

func TestDecodeCutoffsDoc(t *testing.T) {
	t.Run("null is no cutoffs", func(t *testing.T) {
		cutoffs, err := decodeCutoffs([]byte("null"))
		if err != nil {
			t.Fatalf("decodeCutoffs(null): %v", err)
		}
		if cutoffs != nil {
			t.Fatalf("decodeCutoffs(null) = %v, want nil", cutoffs)
		}
	})
	t.Run("days and maxOrders are kept", func(t *testing.T) {
		cutoffs, err := decodeCutoffs([]byte(`[{"localTime":"18:00","days":["monday"],"maxOrders":50}]`))
		if err != nil {
			t.Fatalf("decodeCutoffs(): %v", err)
		}
		if len(cutoffs) != 1 || cutoffs[0].LocalTime != "18:00" || cutoffs[0].MaxOrders != 50 || len(cutoffs[0].Days) != 1 {
			t.Fatalf("decodeCutoffs() = %+v, want every field", cutoffs)
		}
	})
	t.Run("corrupt JSON is an error", func(t *testing.T) {
		if _, err := decodeCutoffs([]byte("[")); err == nil {
			t.Fatal("decodeCutoffs of corrupt JSON = no error")
		}
	})
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Run("nil hours encode as null", func(t *testing.T) {
		raw, err := encodeHours(nil)
		if err != nil {
			t.Fatalf("encodeHours(nil): %v", err)
		}
		if string(raw) != "null" {
			t.Fatalf("encodeHours(nil) = %s, want null", raw)
		}
	})
	t.Run("nil cutoffs encode as null", func(t *testing.T) {
		raw, err := encodeCutoffs(nil)
		if err != nil {
			t.Fatalf("encodeCutoffs(nil): %v", err)
		}
		if string(raw) != "null" {
			t.Fatalf("encodeCutoffs(nil) = %s, want null", raw)
		}
	})
	t.Run("hours survive encode-decode", func(t *testing.T) {
		roundTripHours(t)
	})
	t.Run("cutoffs survive encode-decode", func(t *testing.T) {
		roundTripCutoffs(t)
	})
}

func roundTripHours(t *testing.T) {
	t.Helper()
	in := node.OperatingHours{
		node.Tuesday: {Start: "07:30", End: "21:15"},
		node.Sunday:  {Start: "09:00", End: "13:00"},
	}
	raw, err := encodeHours(in)
	if err != nil {
		t.Fatalf("encodeHours(): %v", err)
	}
	out, err := decodeHours(raw)
	if err != nil {
		t.Fatalf("decodeHours(): %v", err)
	}
	if len(out) != len(in) || out[node.Tuesday] != in[node.Tuesday] || out[node.Sunday] != in[node.Sunday] {
		t.Fatalf("round trip = %v, want %v", out, in)
	}
}

func roundTripCutoffs(t *testing.T) {
	t.Helper()
	in := []node.CutoffSlot{
		{LocalTime: "11:00"},
		{LocalTime: "18:00", Days: []node.Weekday{node.Friday, node.Saturday}, MaxOrders: 900},
	}
	raw, err := encodeCutoffs(in)
	if err != nil {
		t.Fatalf("encodeCutoffs(): %v", err)
	}
	out, err := decodeCutoffs(raw)
	if err != nil {
		t.Fatalf("decodeCutoffs(): %v", err)
	}
	if len(out) != 2 || out[1].LocalTime != "18:00" || out[1].MaxOrders != 900 || len(out[1].Days) != 2 {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
	if out[0].Days != nil {
		t.Fatalf("round trip gave unset days %v, want nil", out[0].Days)
	}
}

func TestDerefHelpers(t *testing.T) {
	if deref(nil) != "" || deref(ptr("x")) != "x" {
		t.Fatal("deref does not map NULL to the empty string")
	}
	if derefInt(nil) != 0 {
		t.Fatal("derefInt(nil) != 0")
	}
	if v := 7; derefInt(&v) != 7 {
		t.Fatal("derefInt(&7) != 7")
	}
}
