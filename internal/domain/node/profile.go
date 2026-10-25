package node

import (
	"maps"
	"slices"
	"time"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

// Weekday is a day of the week in the model's lowercase spelling.
type Weekday string

// The seven weekdays.
const (
	Monday    Weekday = "monday"
	Tuesday   Weekday = "tuesday"
	Wednesday Weekday = "wednesday"
	Thursday  Weekday = "thursday"
	Friday    Weekday = "friday"
	Saturday  Weekday = "saturday"
	Sunday    Weekday = "sunday"
)

var weekdays = []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday}

// Weekdays returns the seven weekdays, Monday first.
func Weekdays() []Weekday { return slices.Clone(weekdays) }

// Valid reports whether d is one of the seven weekdays.
func (d Weekday) Valid() bool { return slices.Contains(weekdays, d) }

// TimeOfDay is a local time written HH:mm (00:00 to 23:59).
type TimeOfDay string

// Valid reports whether t is a well-formed HH:mm.
func (t TimeOfDay) Valid() bool {
	if len(t) != 5 || t[2] != ':' {
		return false
	}
	return t[0] >= '0' && t[0] <= '2' && t[1] >= '0' && t[1] <= '9' && t[3] >= '0' && t[3] <= '5' &&
		t[4] >= '0' && t[4] <= '9' && t.Minutes() < 24*60
}

// Minutes returns the minutes since midnight of a Valid time.
func (t TimeOfDay) Minutes() int {
	return int(t[0]-'0')*600 + int(t[1]-'0')*60 + int(t[3]-'0')*10 + int(t[4]-'0')
}

// DayHours is the local opening interval of one weekday.
type DayHours struct {
	Start TimeOfDay
	End   TimeOfDay
}

// OperatingHours maps a weekday to its opening hours. A missing weekday is
// closed.
type OperatingHours map[Weekday]DayHours

// operatingDays returns the weekdays with opening hours, Monday first.
func (h OperatingHours) operatingDays() []Weekday {
	var days []Weekday
	for _, d := range weekdays {
		if _, ok := h[d]; ok {
			days = append(days, d)
		}
	}
	return days
}

// CutoffSlot is a local time by which packages are ready for handover; the
// network schedules one pickup departure per cutoff.
type CutoffSlot struct {
	LocalTime TimeOfDay
	// Days lists the weekdays the cutoff applies to; empty means every
	// operating day.
	Days []Weekday
	// MaxOrders caps the orders of the slot; 0 means unset.
	MaxOrders int
}

// TimeUnit is the time unit of a throughput cap.
type TimeUnit string

// The two time units.
const (
	TimeUnitHour TimeUnit = "HOUR"
	TimeUnitDay  TimeUnit = "DAY"
)

// UnitOrder is the only unit a throughput cap counts in.
const UnitOrder = "ORDER"

// ThroughputCap is the declared maximum orders per time unit.
type ThroughputCap struct {
	Value    int
	TimeUnit TimeUnit
	Unit     string
}

// DeclaredBy says who declared a profile revision.
type DeclaredBy string

// The two declarers.
const (
	DeclaredByVendor     DeclaredBy = "VENDOR"
	DeclaredByNetworkOps DeclaredBy = "NETWORK_OPS"
)

// Domain bounds. Plain literals on purpose (Postgres integer ceiling).
const (
	MaxProfileRevision = 2147483647
	MaxMinutes         = 2147483647
	MaxOrders          = 2147483647
	MaxTimezone        = 64
)

// ProfileDeclaration is the input of DeclareOperationalProfile.
type ProfileDeclaration struct {
	Revision            int
	Timezone            string
	OperatingHours      OperatingHours
	Holidays            []string
	HandlingTimeMinutes int
	Cutoffs             []CutoffSlot
	ThroughputCap       ThroughputCap
	// SortDwellMinutes is declared only by SORTATION_CENTER and AIR_HUB nodes.
	SortDwellMinutes *int
	// Inbound is the optional inbound limits of the node; nil declares none
	// (ADR 0007).
	Inbound    *InboundLimits
	DeclaredBy DeclaredBy
}

// OperationalProfile is one immutable, versioned revision of a node's
// declared operational profile. Build it with NewOperationalProfile.
type OperationalProfile struct {
	ProfileDeclaration
	DeclaredAt time.Time
}

// Complete reports whether the profile has at least one cutoff and a handling
// time above zero, which a FULFILLMENT_CENTER needs to be (or stay) ACTIVE.
func (p OperationalProfile) Complete() bool {
	return len(p.Cutoffs) > 0 && p.HandlingTimeMinutes > 0
}

// clone returns a deep copy.
func (p OperationalProfile) clone() OperationalProfile {
	c := p
	c.OperatingHours = maps.Clone(p.OperatingHours)
	c.Holidays = slices.Clone(p.Holidays)
	c.Cutoffs = make([]CutoffSlot, len(p.Cutoffs))
	for i, s := range p.Cutoffs {
		s.Days = slices.Clone(s.Days)
		c.Cutoffs[i] = s
	}
	if p.SortDwellMinutes != nil {
		v := *p.SortDwellMinutes
		c.SortDwellMinutes = &v
	}
	c.Inbound = p.Inbound.clone()
	return c
}

// NewOperationalProfile validates a declaration and returns the revision with
// declaredAt. Value errors are ErrValidation; a cutoff outside the operating
// hours of one of its days is ErrCutoffOutsideOperatingHours.
func NewOperationalProfile(d ProfileDeclaration, declaredAt time.Time) (OperationalProfile, error) {
	p := OperationalProfile{ProfileDeclaration: d, DeclaredAt: shared.UTC(declaredAt)}.clone()
	if p.Holidays == nil {
		p.Holidays = []string{}
	}
	if p.OperatingHours == nil {
		p.OperatingHours = OperatingHours{}
	}
	slices.Sort(p.Holidays)
	if err := checkShape(p); err != nil {
		return OperationalProfile{}, shared.Invalid("%v", err)
	}
	if err := checkCutoffs(p); err != nil {
		return OperationalProfile{}, err
	}
	return p, nil
}
