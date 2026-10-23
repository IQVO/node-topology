package node

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

var (
	t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
	t2 = t0.Add(2 * time.Hour)
	t3 = t0.Add(3 * time.Hour)
)

func intp(v int) *int { return &v }

func workWeek() OperatingHours {
	h := OperatingHours{Saturday: {Start: "08:00", End: "14:00"}}
	for _, d := range []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday} {
		h[d] = DayHours{Start: "06:00", End: "22:00"}
	}
	return h
}

// validDecl is the example of the domain-events page: revision 1, Mon-Sat
// hours, an 11:00 cutoff for every operating day and an 18:00 one Mon-Fri.
func validDecl() ProfileDeclaration {
	return ProfileDeclaration{
		Revision:            1,
		Timezone:            "America/Sao_Paulo",
		OperatingHours:      workWeek(),
		Holidays:            []string{"2026-12-25"},
		HandlingTimeMinutes: 240,
		Cutoffs: []CutoffSlot{
			{LocalTime: "11:00"},
			{LocalTime: "18:00", Days: []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday}, MaxOrders: 1200},
		},
		ThroughputCap: ThroughputCap{Value: 400, TimeUnit: TimeUnitHour, Unit: UnitOrder},
		DeclaredBy:    DeclaredByVendor,
	}
}

func TestNewOperationalProfileHappyPath(t *testing.T) {
	brt := time.FixedZone("BRT", -3*3600)
	p, err := NewOperationalProfile(validDecl(), t0.In(brt))
	if err != nil {
		t.Fatal(err)
	}
	want := OperationalProfile{ProfileDeclaration: validDecl(), DeclaredAt: t0}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("profile = %+v\nwant     %+v", p, want)
	}
	if p.DeclaredAt.Location() != time.UTC {
		t.Fatal("declaredAt must be UTC")
	}
	if !p.Complete() {
		t.Fatal("two cutoffs and 240 minutes is complete")
	}
}

func TestNewOperationalProfileNormalisesAndCopies(t *testing.T) {
	d := validDecl()
	d.Holidays = []string{"2026-12-25", "2026-01-01", "2026-05-01"}
	d.SortDwellMinutes = intp(30)
	p, err := NewOperationalProfile(d, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Holidays, []string{"2026-01-01", "2026-05-01", "2026-12-25"}) {
		t.Fatalf("holidays must be sorted: %v", p.Holidays)
	}
	if d.Holidays[0] != "2026-12-25" {
		t.Fatal("the caller's holidays must not be reordered")
	}
	// the profile shares nothing with the declaration
	d.OperatingHours[Monday] = DayHours{Start: "01:00", End: "02:00"}
	d.Cutoffs[1].Days[0] = Sunday
	*d.SortDwellMinutes = 99
	if p.OperatingHours[Monday].Start != "06:00" || p.Cutoffs[1].Days[0] != Monday || *p.SortDwellMinutes != 30 {
		t.Fatalf("profile aliased the declaration: %+v", p)
	}
	// and nil collections become empty ones
	e := validDecl()
	e.Holidays, e.OperatingHours, e.Cutoffs = nil, nil, nil
	q, err := NewOperationalProfile(e, t0)
	if err != nil {
		t.Fatal(err)
	}
	if q.Holidays == nil || q.OperatingHours == nil || q.Cutoffs == nil || q.Complete() {
		t.Fatalf("nil collections must be normalised to empty: %+v", q)
	}
}

func decl(mut func(*ProfileDeclaration)) ProfileDeclaration {
	d := validDecl()
	mut(&d)
	return d
}

func TestNewOperationalProfileRejectsInvalidValues(t *testing.T) {
	long := strings.Repeat("a", MaxTimezone+1)
	tests := []struct {
		name string
		d    ProfileDeclaration
	}{
		{"revision zero", decl(func(d *ProfileDeclaration) { d.Revision = 0 })},
		{"revision negative", decl(func(d *ProfileDeclaration) { d.Revision = -1 })},
		{"revision above int32", decl(func(d *ProfileDeclaration) { d.Revision = MaxProfileRevision + 1 })},
		{"empty timezone", decl(func(d *ProfileDeclaration) { d.Timezone = "" })},
		{"timezone too long", decl(func(d *ProfileDeclaration) { d.Timezone = long })},
		{"timezone starting with digit", decl(func(d *ProfileDeclaration) { d.Timezone = "1Sao_Paulo" })},
		{"timezone with space", decl(func(d *ProfileDeclaration) { d.Timezone = "America/Sao Paulo" })},
		{"timezone leading slash", decl(func(d *ProfileDeclaration) { d.Timezone = "/Sao_Paulo" })},
		{"timezone empty segment", decl(func(d *ProfileDeclaration) { d.Timezone = "America//Sao_Paulo" })},
		{"timezone trailing slash", decl(func(d *ProfileDeclaration) { d.Timezone = "America/" })},
		{"unknown weekday", decl(func(d *ProfileDeclaration) { d.OperatingHours["funday"] = DayHours{Start: "06:00", End: "07:00"} })},
		{"bad opening time", decl(func(d *ProfileDeclaration) { d.OperatingHours[Monday] = DayHours{Start: "6:00", End: "22:00"} })},
		{"bad closing time", decl(func(d *ProfileDeclaration) { d.OperatingHours[Monday] = DayHours{Start: "06:00", End: "24:00"} })},
		{"closing equals opening", decl(func(d *ProfileDeclaration) { d.OperatingHours[Monday] = DayHours{Start: "06:00", End: "06:00"} })},
		{"closing before opening", decl(func(d *ProfileDeclaration) { d.OperatingHours[Monday] = DayHours{Start: "22:00", End: "06:00"} })},
		{"holiday not a date", decl(func(d *ProfileDeclaration) { d.Holidays = []string{"25/12/2026"} })},
		{"holiday without zero padding", decl(func(d *ProfileDeclaration) { d.Holidays = []string{"2026-1-5"} })},
		{"holiday impossible date", decl(func(d *ProfileDeclaration) { d.Holidays = []string{"2026-02-30"} })},
		{"holiday repeated", decl(func(d *ProfileDeclaration) { d.Holidays = []string{"2026-12-25", "2026-12-25"} })},
		{"handling negative", decl(func(d *ProfileDeclaration) { d.HandlingTimeMinutes = -1 })},
		{"handling above int32", decl(func(d *ProfileDeclaration) { d.HandlingTimeMinutes = MaxMinutes + 1 })},
		{"sort dwell negative", decl(func(d *ProfileDeclaration) { d.SortDwellMinutes = intp(-1) })},
		{"sort dwell above int32", decl(func(d *ProfileDeclaration) { d.SortDwellMinutes = intp(MaxMinutes + 1) })},
		{"declaredBy empty", decl(func(d *ProfileDeclaration) { d.DeclaredBy = "" })},
		{"declaredBy unknown", decl(func(d *ProfileDeclaration) { d.DeclaredBy = "PLANNER" })},
		{"cap zero", decl(func(d *ProfileDeclaration) { d.ThroughputCap.Value = 0 })},
		{"cap above int32", decl(func(d *ProfileDeclaration) { d.ThroughputCap.Value = MaxOrders + 1 })},
		{"cap unit unknown", decl(func(d *ProfileDeclaration) { d.ThroughputCap.TimeUnit = "WEEK" })},
		{"cap unit empty", decl(func(d *ProfileDeclaration) { d.ThroughputCap.TimeUnit = "" })},
		{"cap counts something else", decl(func(d *ProfileDeclaration) { d.ThroughputCap.Unit = "PARCEL" })},
		{"cap counts nothing", decl(func(d *ProfileDeclaration) { d.ThroughputCap.Unit = "" })},
		{"cutoff time malformed", decl(func(d *ProfileDeclaration) { d.Cutoffs[0].LocalTime = "11h" })},
		{"cutoff maxOrders negative", decl(func(d *ProfileDeclaration) { d.Cutoffs[1].MaxOrders = -1 })},
		{"cutoff maxOrders above int32", decl(func(d *ProfileDeclaration) { d.Cutoffs[1].MaxOrders = MaxOrders + 1 })},
		{"cutoff unknown day", decl(func(d *ProfileDeclaration) { d.Cutoffs[1].Days = []Weekday{"someday"} })},
		{"cutoff repeated day", decl(func(d *ProfileDeclaration) { d.Cutoffs[1].Days = []Weekday{Monday, Monday} })},
		{"same cutoff twice on every day", decl(func(d *ProfileDeclaration) {
			d.Cutoffs = []CutoffSlot{{LocalTime: "11:00"}, {LocalTime: "11:00"}}
		})},
		{"same cutoff twice on a shared day", decl(func(d *ProfileDeclaration) {
			d.Cutoffs = []CutoffSlot{{LocalTime: "11:00", Days: []Weekday{Monday, Tuesday}}, {LocalTime: "11:00", Days: []Weekday{Tuesday}}}
		})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOperationalProfile(tc.d, t0)
			if !errors.Is(err, shared.ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
	if _, err := NewOperationalProfile(validDecl(), time.Time{}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("a zero declaredAt is ErrValidation, got %v", err)
	}
}

func TestNewOperationalProfileBoundariesAreInclusive(t *testing.T) {
	ok := []struct {
		name string
		mut  func(*ProfileDeclaration)
	}{
		{"revision 1", func(d *ProfileDeclaration) { d.Revision = 1 }},
		{"revision max", func(d *ProfileDeclaration) { d.Revision = MaxProfileRevision }},
		{"handling zero", func(d *ProfileDeclaration) { d.HandlingTimeMinutes = 0 }},
		{"handling max", func(d *ProfileDeclaration) { d.HandlingTimeMinutes = MaxMinutes }},
		{"sort dwell zero", func(d *ProfileDeclaration) { d.SortDwellMinutes = intp(0) }},
		{"sort dwell max", func(d *ProfileDeclaration) { d.SortDwellMinutes = intp(MaxMinutes) }},
		{"cap 1 per day", func(d *ProfileDeclaration) {
			d.ThroughputCap = ThroughputCap{Value: 1, TimeUnit: TimeUnitDay, Unit: UnitOrder}
		}},
		{"cap max", func(d *ProfileDeclaration) { d.ThroughputCap.Value = MaxOrders }},
		{"maxOrders 1", func(d *ProfileDeclaration) { d.Cutoffs[1].MaxOrders = 1 }},
		{"maxOrders max", func(d *ProfileDeclaration) { d.Cutoffs[1].MaxOrders = MaxOrders }},
		{"declared by network ops", func(d *ProfileDeclaration) { d.DeclaredBy = DeclaredByNetworkOps }},
		{"timezone UTC", func(d *ProfileDeclaration) { d.Timezone = "UTC" }},
		{"timezone with plus and digits", func(d *ProfileDeclaration) { d.Timezone = "Etc/GMT+3" }},
		{"timezone three segments", func(d *ProfileDeclaration) { d.Timezone = "America/Argentina/Buenos_Aires" }},
		{"timezone with dash", func(d *ProfileDeclaration) { d.Timezone = "America/Port-au-Prince" }},
		{"timezone at the limit", func(d *ProfileDeclaration) { d.Timezone = strings.Repeat("a", MaxTimezone) }},
		{"timezone letters at the range edges", func(d *ProfileDeclaration) { d.Timezone = "aZ/zA/z0/a9" }},
		{"one minute of opening", func(d *ProfileDeclaration) {
			d.OperatingHours = OperatingHours{Monday: {Start: "10:00", End: "10:01"}}
			d.Cutoffs = []CutoffSlot{{LocalTime: "10:00"}}
		}},
		{"no operating hours and no cutoff", func(d *ProfileDeclaration) { d.OperatingHours, d.Cutoffs = nil, nil }},
		{"same cutoff time on disjoint days", func(d *ProfileDeclaration) {
			d.Cutoffs = []CutoffSlot{{LocalTime: "11:00", Days: []Weekday{Monday}}, {LocalTime: "11:00", Days: []Weekday{Tuesday}}}
		}},
		{"two cutoff times on the same day", func(d *ProfileDeclaration) {
			d.Cutoffs = []CutoffSlot{{LocalTime: "11:00"}, {LocalTime: "11:01"}}
		}},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewOperationalProfile(decl(tc.mut), t0); err != nil {
				t.Fatalf("must be accepted: %v", err)
			}
		})
	}
}

func TestTimezoneCharacterClasses(t *testing.T) {
	for _, tz := range []string{"a", "z", "A", "Z", "aa0", "aa9", "a_", "a-", "a+", "a/b"} {
		if !validTimezone(tz) {
			t.Errorf("%q must be valid", tz)
		}
	}
	for _, tz := range []string{"@", "[", "`", "{", "a/", "a:", "a.", "a,", "a b", "a\x00", "a/ ", "0a", "-a", "_a", "a//b", "é"} {
		if validTimezone(tz) {
			t.Errorf("%q must be invalid", tz)
		}
	}
	if validTimezone("a:") || validTimezone("a/0/:") || validTimezone("a/:") || validTimezone("a/@") || validTimezone("a/[") || validTimezone("a/`") || validTimezone("a/{") {
		t.Error("characters just outside the accepted ranges must be refused after a slash too")
	}
}

func TestTimeOfDay(t *testing.T) {
	good := map[TimeOfDay]int{"00:00": 0, "00:01": 1, "09:30": 570, "10:00": 600, "19:59": 1199, "20:00": 1200, "23:59": 1439, "12:34": 754}
	for s, minutes := range good {
		if !s.Valid() {
			t.Errorf("%q must be valid", s)
		}
		if got := s.Minutes(); got != minutes {
			t.Errorf("%q.Minutes() = %d, want %d", s, got, minutes)
		}
	}
	bad := []TimeOfDay{"", "9:30", "09:3", "09:300", "009:30", "24:00", "25:00", "29:59", "30:00", "/0:00", "0/:00", "0:00", "12:60", "12:6a", "12:/0",
		"12:0/", "12:0:", "1a:00", "ab:cd", "12-00", "12 00", "1:2:3", "-1:00", "12:5", "12:"}
	for _, s := range bad {
		if s.Valid() {
			t.Errorf("%q must be invalid", s)
		}
	}
}

func TestWeekdays(t *testing.T) {
	want := []Weekday{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	got := Weekdays()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Weekdays() = %v", got)
	}
	got[0] = Sunday
	if Weekdays()[0] != Monday {
		t.Fatal("Weekdays() must return a copy")
	}
	for _, d := range want {
		if !d.Valid() {
			t.Errorf("%s must be valid", d)
		}
	}
	if Weekday("").Valid() || Weekday("Monday").Valid() {
		t.Error("unknown weekdays must be invalid")
	}
}

func TestCutoffWindowIsInclusiveAndPerDay(t *testing.T) {
	hours := OperatingHours{Monday: {Start: "06:00", End: "22:00"}, Saturday: {Start: "08:00", End: "14:00"}}
	tests := []struct {
		name    string
		cutoff  CutoffSlot
		outside bool
	}{
		{"at opening", CutoffSlot{LocalTime: "06:00", Days: []Weekday{Monday}}, false},
		{"at closing", CutoffSlot{LocalTime: "22:00", Days: []Weekday{Monday}}, false},
		{"one minute before opening", CutoffSlot{LocalTime: "05:59", Days: []Weekday{Monday}}, true},
		{"one minute after closing", CutoffSlot{LocalTime: "22:01", Days: []Weekday{Monday}}, true},
		{"a closed day", CutoffSlot{LocalTime: "10:00", Days: []Weekday{Tuesday}}, true},
		{"open on monday but not on saturday", CutoffSlot{LocalTime: "18:00", Days: []Weekday{Monday, Saturday}}, true},
		{"default days include saturday", CutoffSlot{LocalTime: "18:00"}, true},
		{"default days all fit", CutoffSlot{LocalTime: "09:00"}, false},
		{"default days: inside only one", CutoffSlot{LocalTime: "07:00"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := decl(func(d *ProfileDeclaration) { d.OperatingHours, d.Cutoffs = hours, []CutoffSlot{tc.cutoff} })
			_, err := NewOperationalProfile(d, t0)
			if tc.outside != errors.Is(err, ErrCutoffOutsideOperatingHours) {
				t.Fatalf("outside = %v, got %v", tc.outside, err)
			}
			if err != nil && errors.Is(err, shared.ErrValidation) {
				t.Fatal("a cutoff outside the hours is its own error, not ErrValidation")
			}
		})
	}
	// a cutoff with the default days and no operating day at all applies to nothing
	d := decl(func(d *ProfileDeclaration) {
		d.OperatingHours, d.Cutoffs = OperatingHours{}, []CutoffSlot{{LocalTime: "10:00"}}
	})
	if _, err := NewOperationalProfile(d, t0); !errors.Is(err, ErrCutoffOutsideOperatingHours) {
		t.Fatalf("want ErrCutoffOutsideOperatingHours, got %v", err)
	}
}

func TestShapeIsCheckedBeforeCutoffWindows(t *testing.T) {
	d := decl(func(d *ProfileDeclaration) {
		d.Cutoffs = []CutoffSlot{{LocalTime: "03:00"}}
		d.ThroughputCap.Value = 0
	})
	_, err := NewOperationalProfile(d, t0)
	if !errors.Is(err, shared.ErrValidation) || errors.Is(err, ErrCutoffOutsideOperatingHours) {
		t.Fatalf("shape errors come first: %v", err)
	}
}

func TestProfileCompleteness(t *testing.T) {
	tests := []struct {
		name     string
		cutoffs  int
		handling int
		want     bool
	}{
		{"cutoff and handling", 1, 1, true},
		{"many cutoffs", 3, 240, true},
		{"no cutoff", 0, 240, false},
		{"no handling", 1, 0, false},
		{"neither", 0, 0, false},
	}
	for _, tc := range tests {
		p := OperationalProfile{ProfileDeclaration: ProfileDeclaration{
			Cutoffs: make([]CutoffSlot, tc.cutoffs), HandlingTimeMinutes: tc.handling,
		}}
		if got := p.Complete(); got != tc.want {
			t.Errorf("%s: Complete() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
