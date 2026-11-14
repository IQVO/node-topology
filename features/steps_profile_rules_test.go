package features_test

import (
	"strings"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/domain/node"
)

// registerProfileRuleSteps adds the declaration variants the profile rule
// features need: a Monday-to-Saturday week with a short Saturday, a throughput
// cap of any shape, a holiday and a time zone.
func registerProfileRuleSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" open Monday to Friday 06:00-22:00 and Saturday 08:00-14:00 with a cutoff at "([^"]*)" on every operating day$`, w.declareSaturdayWeekDefaultDays)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" open Monday to Friday 06:00-22:00 and Saturday 08:00-14:00 with a cutoff at "([^"]*)" on "([a-z,]+)"$`, w.declareSaturdayWeekDays)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with a throughput cap of (-?\d+) ([A-Z]+) per ([A-Z]+)$`, w.declareWithCap)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with the holiday "([^"]*)"$`, w.declareWithHoliday)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" in the time zone "([^"]*)"$`, w.declareInTimezone)
}

// saturdayWeekDeclaration is open Monday to Friday 06:00-22:00 and Saturday
// 08:00-14:00 (the example of the domain-events page), closed on Sunday.
func saturdayWeekDeclaration(revision int, cutoff node.CutoffSlot) node.ProfileDeclaration {
	d := weekdayDeclaration(revision)
	d.OperatingHours[node.Saturday] = node.DayHours{Start: "08:00", End: "14:00"}
	d.Cutoffs = []node.CutoffSlot{cutoff}
	return d
}

func (w *world) declareSaturdayWeekDefaultDays(revision int, alias, at string) error {
	return w.declareAndRemember(alias, saturdayWeekDeclaration(revision, node.CutoffSlot{LocalTime: node.TimeOfDay(at)}))
}

func (w *world) declareSaturdayWeekDays(revision int, alias, at, days string) error {
	slot := node.CutoffSlot{LocalTime: node.TimeOfDay(at)}
	for _, d := range strings.Split(days, ",") {
		slot.Days = append(slot.Days, node.Weekday(d))
	}
	return w.declareAndRemember(alias, saturdayWeekDeclaration(revision, slot))
}

func (w *world) declareWithCap(revision int, alias string, value int, unit, per string) error {
	d := declaration(revision, 30, 1)
	d.ThroughputCap = node.ThroughputCap{Value: value, TimeUnit: node.TimeUnit(per), Unit: unit}
	return w.declareAndRemember(alias, d)
}

func (w *world) declareWithHoliday(revision int, alias, date string) error {
	d := declaration(revision, 30, 1)
	d.Holidays = []string{date}
	return w.declareAndRemember(alias, d)
}

func (w *world) declareInTimezone(revision int, alias, zone string) error {
	d := declaration(revision, 30, 1)
	d.Timezone = zone
	return w.declareAndRemember(alias, d)
}
