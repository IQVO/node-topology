package node

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

// checkShape validates every value of a profile that does not depend on the
// node it belongs to. Errors are plain: callers wrap them.
func checkShape(p OperationalProfile) error {
	checks := []func(OperationalProfile) error{
		checkRevision, checkTimezone, checkHours, checkHolidays, checkScalars, checkCap, checkCutoffShape,
	}
	for _, check := range checks {
		if err := check(p); err != nil {
			return err
		}
	}
	return nil
}

func checkRevision(p OperationalProfile) error {
	if p.Revision < 1 || p.Revision > MaxProfileRevision {
		return fmt.Errorf("profileRevision must be between 1 and %d", MaxProfileRevision)
	}
	return nil
}

func checkTimezone(p OperationalProfile) error {
	if !validTimezone(p.Timezone) {
		return errors.New("timezone must look like an IANA name such as America/Sao_Paulo")
	}
	return nil
}

// validTimezone checks the shape of an IANA name (ADR 0002, A1): segments of
// letters, digits, _ - + separated by /, the first character a letter.
func validTimezone(s string) bool {
	if s == "" || len(s) > MaxTimezone || !isLetter(s[0]) {
		return false
	}
	segment := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' {
			if segment == 0 {
				return false
			}
			segment = 0
			continue
		}
		if !isLetter(c) && (c < '0' || c > '9') && c != '_' && c != '-' && c != '+' {
			return false
		}
		segment++
	}
	return segment > 0
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func checkHours(p OperationalProfile) error {
	for day, h := range p.OperatingHours {
		if !day.Valid() {
			return fmt.Errorf("operatingHoursByDay has unknown weekday %q", day)
		}
		if !h.Start.Valid() || !h.End.Valid() {
			return fmt.Errorf("operating hours of %s must be HH:mm", day)
		}
		if h.Start.Minutes() >= h.End.Minutes() {
			return fmt.Errorf("operating hours of %s must open before they close", day)
		}
	}
	return nil
}

const isoDate = "2006-01-02"

func checkHolidays(p OperationalProfile) error {
	for i, h := range p.Holidays {
		if d, err := time.Parse(isoDate, h); err != nil || d.Format(isoDate) != h {
			return fmt.Errorf("holiday %q is not an ISO date", h)
		}
		if i > 0 && h <= p.Holidays[i-1] {
			return fmt.Errorf("holiday %q is repeated or out of order", h)
		}
	}
	return nil
}

func checkScalars(p OperationalProfile) error {
	if p.HandlingTimeMinutes < 0 || p.HandlingTimeMinutes > MaxMinutes {
		return fmt.Errorf("handlingTimeMinutes must be between 0 and %d", MaxMinutes)
	}
	if d := p.SortDwellMinutes; d != nil && (*d < 0 || *d > MaxMinutes) {
		return fmt.Errorf("sortDwellMinutes must be between 0 and %d", MaxMinutes)
	}
	if p.DeclaredBy != DeclaredByVendor && p.DeclaredBy != DeclaredByNetworkOps {
		return fmt.Errorf("declaredBy must be VENDOR or NETWORK_OPS, got %q", p.DeclaredBy)
	}
	if p.DeclaredAt.IsZero() {
		return errors.New("declaredAt is required")
	}
	return nil
}

func checkCap(p OperationalProfile) error {
	c := p.ThroughputCap
	if c.Value < 1 || c.Value > MaxOrders {
		return fmt.Errorf("throughputCap.value must be between 1 and %d", MaxOrders)
	}
	if c.TimeUnit != TimeUnitHour && c.TimeUnit != TimeUnitDay {
		return fmt.Errorf("throughputCap.timeUnit must be HOUR or DAY, got %q", c.TimeUnit)
	}
	if c.Unit != UnitOrder {
		return fmt.Errorf("throughputCap.unit must be %s, got %q", UnitOrder, c.Unit)
	}
	return nil
}

func checkCutoffShape(p OperationalProfile) error {
	for _, c := range p.Cutoffs {
		if !c.LocalTime.Valid() {
			return fmt.Errorf("cutoff localTime %q must be HH:mm", c.LocalTime)
		}
		if c.MaxOrders < 0 || c.MaxOrders > MaxOrders {
			return fmt.Errorf("cutoff %s maxOrders must be between 1 and %d when set", c.LocalTime, MaxOrders)
		}
		seen := map[Weekday]bool{}
		for _, d := range c.Days {
			if !d.Valid() || seen[d] {
				return fmt.Errorf("cutoff %s days must be distinct weekdays, got %q", c.LocalTime, d)
			}
			seen[d] = true
		}
	}
	return nil
}

// effectiveDays is the weekdays a cutoff applies to: its own, or every
// operating day when it names none.
func (c CutoffSlot) effectiveDays(h OperatingHours) []Weekday {
	if len(c.Days) > 0 {
		return c.Days
	}
	return h.operatingDays()
}

// checkCutoffs enforces ErrCutoffOutsideOperatingHours and refuses two cutoffs
// that would schedule the same departure (ADR 0002, A4).
func checkCutoffs(p OperationalProfile) error {
	eff := make([][]Weekday, len(p.Cutoffs))
	for i, c := range p.Cutoffs {
		eff[i] = c.effectiveDays(p.OperatingHours)
		if err := checkWindow(c, eff[i], p.OperatingHours); err != nil {
			return err
		}
	}
	for i := range p.Cutoffs {
		for j := i + 1; j < len(p.Cutoffs); j++ {
			if p.Cutoffs[i].LocalTime == p.Cutoffs[j].LocalTime && intersects(eff[i], eff[j]) {
				return shared.Invalid("cutoffs %d and %d are both at %s on a shared day", i, j, p.Cutoffs[i].LocalTime)
			}
		}
	}
	return nil
}

func checkWindow(c CutoffSlot, days []Weekday, h OperatingHours) error {
	if len(days) == 0 {
		return fmt.Errorf("%w: cutoff %s applies to no operating day", ErrCutoffOutsideOperatingHours, c.LocalTime)
	}
	at := c.LocalTime.Minutes()
	for _, d := range days {
		hours, open := h[d]
		if !open || at < hours.Start.Minutes() || at > hours.End.Minutes() {
			return fmt.Errorf("%w: cutoff %s is outside the operating hours of %s", ErrCutoffOutsideOperatingHours, c.LocalTime, d)
		}
	}
	return nil
}

func intersects(a, b []Weekday) bool {
	return slices.ContainsFunc(a, func(d Weekday) bool { return slices.Contains(b, d) })
}
