package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/claudioed/node-topology/internal/domain/node"
)

// The JSON documents of node_profile_revisions. They are owned by this adapter
// (explicit tags), not borrowed from the domain or the REST views, so a
// refactor of either cannot silently change what is stored.

type dayHoursDoc struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type cutoffDoc struct {
	LocalTime string   `json:"localTime"`
	Days      []string `json:"days"`
	MaxOrders int      `json:"maxOrders"`
}

// profileRow holds the columns of one node_profile_revisions row. Every column
// is a pointer or a raw document because the same shape is read through a LEFT
// JOIN, where a node without a profile yields all NULLs (Revision == nil).
type profileRow struct {
	Revision         *int
	Timezone         *string
	OperatingHours   []byte
	Holidays         []byte
	HandlingTime     *int
	Cutoffs          []byte
	ThroughputValue  *int
	ThroughputUnitOf *string
	ThroughputUnit   *string
	SortDwell        *int
	DeclaredBy       *string
	DeclaredAt       *time.Time
}

// profileColumns lists the columns of node_profile_revisions under the alias
// p, in the order dest scans them.
const profileColumns = `p.revision, p.timezone, p.operating_hours, p.holidays, p.handling_time_minutes, p.cutoffs,
	p.throughput_value, p.throughput_time_unit, p.throughput_unit, p.sort_dwell_minutes, p.declared_by, p.declared_at`

// dest returns the scan targets matching profileColumns.
func (p *profileRow) dest() []any {
	return []any{
		&p.Revision, &p.Timezone, &p.OperatingHours, &p.Holidays, &p.HandlingTime, &p.Cutoffs,
		&p.ThroughputValue, &p.ThroughputUnitOf, &p.ThroughputUnit, &p.SortDwell, &p.DeclaredBy, &p.DeclaredAt,
	}
}

// profile rebuilds the revision through the domain constructor, which
// validates it, so a corrupt row can never become a live profile. ok is false
// when the row is the all-NULL side of a LEFT JOIN.
func (p *profileRow) profile() (_ node.OperationalProfile, ok bool, _ error) {
	if p.Revision == nil {
		return node.OperationalProfile{}, false, nil
	}
	hours, err := decodeHours(p.OperatingHours)
	if err != nil {
		return node.OperationalProfile{}, false, err
	}
	cutoffs, err := decodeCutoffs(p.Cutoffs)
	if err != nil {
		return node.OperationalProfile{}, false, err
	}
	var holidays []string
	if err := json.Unmarshal(p.Holidays, &holidays); err != nil {
		return node.OperationalProfile{}, false, fmt.Errorf("decode holidays: %w", err)
	}
	decl := node.ProfileDeclaration{
		Revision:            *p.Revision,
		Timezone:            deref(p.Timezone),
		OperatingHours:      hours,
		Holidays:            holidays,
		HandlingTimeMinutes: derefInt(p.HandlingTime),
		Cutoffs:             cutoffs,
		ThroughputCap: node.ThroughputCap{
			Value: derefInt(p.ThroughputValue), TimeUnit: node.TimeUnit(deref(p.ThroughputUnitOf)), Unit: deref(p.ThroughputUnit),
		},
		SortDwellMinutes: p.SortDwell,
		DeclaredBy:       node.DeclaredBy(deref(p.DeclaredBy)),
	}
	prof, err := node.NewOperationalProfile(decl, timeOrZero(p.DeclaredAt))
	if err != nil {
		return node.OperationalProfile{}, false, fmt.Errorf("profile revision %d is corrupt: %w", *p.Revision, err)
	}
	return prof, true, nil
}

func deref(s *string) string { return stringOrEmpty(s) }

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func decodeHours(raw []byte) (node.OperatingHours, error) {
	var doc map[string]dayHoursDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode operating hours: %w", err)
	}
	if doc == nil {
		return nil, nil
	}
	out := make(node.OperatingHours, len(doc))
	for day, h := range doc {
		out[node.Weekday(day)] = node.DayHours{Start: node.TimeOfDay(h.Start), End: node.TimeOfDay(h.End)}
	}
	return out, nil
}

func decodeCutoffs(raw []byte) ([]node.CutoffSlot, error) {
	var doc []cutoffDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode cutoffs: %w", err)
	}
	if doc == nil {
		return nil, nil
	}
	out := make([]node.CutoffSlot, len(doc))
	for i, c := range doc {
		out[i] = node.CutoffSlot{LocalTime: node.TimeOfDay(c.LocalTime), MaxOrders: c.MaxOrders}
		if c.Days != nil {
			out[i].Days = make([]node.Weekday, len(c.Days))
			for j, d := range c.Days {
				out[i].Days[j] = node.Weekday(d)
			}
		}
	}
	return out, nil
}

func encodeHours(h node.OperatingHours) ([]byte, error) {
	if h == nil {
		return []byte("null"), nil
	}
	doc := make(map[string]dayHoursDoc, len(h))
	for day, hours := range h {
		doc[string(day)] = dayHoursDoc{Start: string(hours.Start), End: string(hours.End)}
	}
	return json.Marshal(doc)
}

func encodeCutoffs(cs []node.CutoffSlot) ([]byte, error) {
	if cs == nil {
		return []byte("null"), nil
	}
	doc := make([]cutoffDoc, len(cs))
	for i, c := range cs {
		doc[i] = cutoffDoc{LocalTime: string(c.LocalTime), MaxOrders: c.MaxOrders}
		if c.Days != nil {
			doc[i].Days = make([]string, len(c.Days))
			for j, d := range c.Days {
				doc[i].Days[j] = string(d)
			}
		}
	}
	return json.Marshal(doc)
}
