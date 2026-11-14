package features_test

import (
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
)

// registerRulesSteps adds the vocabulary of the documentation-derived rule
// features (*_rules.feature): the state-matrix drivers, the exact capability
// set, cutoff and declarer variants of a profile revision, a reflection-based
// reader of the events the last command published, and keyset-cursor paging.
func registerRulesSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a "([A-Z_]+)" node "([^"]*)" is (PROPOSED|ACTIVE|SUSPENDED|RETIRED) in the region "([^"]*)"$`, w.givenNodeInState)
	sc.Step(`^I send the command "(activate|suspend|retire)" to the node "([^"]*)"$`, w.sendNodeCommand)
	sc.Step(`^the node "([^"]*)" has exactly the capabilities "([a-zA-Z,]*)"$`, w.hasExactCapabilities)

	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" as declared by "(VENDOR|NETWORK_OPS)"$`, w.declareAs)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with a cutoff at "([^"]*)" on "([^"]*)"$`, w.declareOneCutoff)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with a cutoff at "([^"]*)" on "([^"]*)" and a cutoff at "([^"]*)" on "([^"]*)"$`, w.declareTwoCutoffs)

	sc.Step(`^the last command published an? "([A-Za-z]+)" event whose "([A-Za-z.]+)" is "([^"]*)"$`, w.eventFieldIs)
	sc.Step(`^the outbox holds (\d+) "([A-Za-z]+)" events? in total$`, w.outboxHolds)

	sc.Step(`^the next cursor of the response is remembered$`, w.rememberCursor)
	sc.Step(`^a GET request is sent to "([^"]*)" with the remembered cursor$`, w.getWithCursor)
}

// givenNodeInState drives a new node of the type to the state through the
// same use cases the REST adapter calls. A fulfillment center declares a
// complete profile first, since it cannot activate without one.
func (w *world) givenNodeInState(typ, alias, state, regionCode string) error {
	if err := w.givenNode(typ, alias, regionCode); err != nil {
		return err
	}
	if state == string(node.StatusProposed) {
		return nil
	}
	if node.Type(typ) == node.FulfillmentCenter {
		if err := w.given("declare a complete profile of "+alias, func() error { return w.declare(alias, declaration(1, 30, 1)) }); err != nil {
			return err
		}
	}
	steps := []string{"activate"}
	switch state {
	case string(node.StatusSuspended):
		steps = append(steps, "suspend")
	case string(node.StatusRetired):
		steps = append(steps, "retire")
	}
	for _, step := range steps {
		if err := w.given(step+" "+alias, func() error { return w.nodeCommand(step, alias) }); err != nil {
			return err
		}
	}
	return nil
}

// nodeCommand runs one lifecycle command by name; a suspension needs a reason.
func (w *world) nodeCommand(command, alias string) error {
	id, err := w.nodeID(alias)
	if err != nil {
		return err
	}
	switch command {
	case "activate":
		_, err = usecases.NewActivateNode(w.deps).Execute(w.ctx(), id)
	case "suspend":
		_, err = usecases.NewSuspendNode(w.deps).Execute(w.ctx(), id, "routine inspection")
	case "retire":
		_, err = usecases.NewRetireNode(w.deps).Execute(w.ctx(), id)
	default:
		return fmt.Errorf("unknown node command %q", command)
	}
	return err
}

func (w *world) sendNodeCommand(command, alias string) error {
	if _, err := w.nodeID(alias); err != nil {
		return err
	}
	w.run(func() error { return w.nodeCommand(command, alias) })
	return nil
}

func (w *world) hasExactCapabilities(alias, list string) error {
	v, err := w.nodeView(alias)
	if err != nil {
		return err
	}
	got := []string{}
	for name, on := range map[string]bool{
		"outbound": v.Capabilities.Outbound, "inbound": v.Capabilities.Inbound, "returns": v.Capabilities.Returns,
		"sort": v.Capabilities.Sort, "lastMile": v.Capabilities.LastMile, "air": v.Capabilities.Air,
	} {
		if on {
			got = append(got, name)
		}
	}
	want := []string{}
	if list != "" {
		want = strings.Split(list, ",")
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		return fmt.Errorf("node %s has the capabilities %v, want exactly %v", alias, got, want)
	}
	return nil
}

// weekdayDeclaration is a valid revision open Monday to Friday, 06:00-22:00,
// with a single 14:00 Monday cutoff.
func weekdayDeclaration(revision int) node.ProfileDeclaration {
	d := repocontract.Declaration(revision)
	d.OperatingHours = node.OperatingHours{}
	for _, day := range []node.Weekday{node.Monday, node.Tuesday, node.Wednesday, node.Thursday, node.Friday} {
		d.OperatingHours[day] = node.DayHours{Start: "06:00", End: "22:00"}
	}
	return d
}

func (w *world) declareAs(revision int, alias, by string) error {
	d := declaration(revision, 30, 1)
	d.DeclaredBy = node.DeclaredBy(by)
	return w.declareAndRemember(alias, d)
}

func (w *world) declareCutoffs(revision int, alias string, cutoffs ...node.CutoffSlot) error {
	d := weekdayDeclaration(revision)
	d.Cutoffs = cutoffs
	return w.declareAndRemember(alias, d)
}

func (w *world) declareOneCutoff(revision int, alias, at, day string) error {
	return w.declareCutoffs(revision, alias, node.CutoffSlot{LocalTime: node.TimeOfDay(at), Days: []node.Weekday{node.Weekday(day)}})
}

func (w *world) declareTwoCutoffs(revision int, alias, at1, day1, at2, day2 string) error {
	return w.declareCutoffs(revision, alias,
		node.CutoffSlot{LocalTime: node.TimeOfDay(at1), Days: []node.Weekday{node.Weekday(day1)}},
		node.CutoffSlot{LocalTime: node.TimeOfDay(at2), Days: []node.Weekday{node.Weekday(day2)}})
}

// eventFieldIs reads one field (a dotted path through the event struct) of
// the named event among those the last command published, and compares its
// text to the expectation. node:/lane:/region: aliases in the expectation
// are resolved to ids.
func (w *world) eventFieldIs(name, path, want string) error {
	want, err := w.resolveAliases(want)
	if err != nil {
		return err
	}
	for _, e := range w.store.Outbox().Events()[w.outboxBefore:] {
		if e.EventName() != name {
			continue
		}
		got, err := fieldText(reflect.ValueOf(e), path)
		if err != nil {
			return fmt.Errorf("%s event: %w", name, err)
		}
		if got != want {
			return fmt.Errorf("%s event field %s = %q, want %q", name, path, got, want)
		}
		return nil
	}
	return fmt.Errorf("the last command published %v, no %s event among them", w.published(), name)
}

func fieldText(v reflect.Value, path string) (string, error) {
	for _, part := range strings.Split(path, ".") {
		for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			if v.IsNil() {
				return "<nil>", nil
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return "", fmt.Errorf("%s is not a struct", part)
		}
		v = v.FieldByName(part)
		if !v.IsValid() {
			return "", fmt.Errorf("no field %s", part)
		}
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "<nil>", nil
		}
		v = v.Elem()
	}
	if t, ok := v.Interface().(time.Time); ok {
		return t.UTC().Format(time.RFC3339Nano), nil
	}
	return fmt.Sprint(v.Interface()), nil
}

func (w *world) outboxHolds(want int, name string) error {
	got := 0
	for _, e := range w.store.Outbox().Events() {
		if e.EventName() == name {
			got++
		}
	}
	if got != want {
		return fmt.Errorf("the outbox holds %d %s events, want %d", got, name, want)
	}
	return nil
}

func (w *world) rememberCursor() error {
	if w.httpw == nil {
		return errNoCommand
	}
	doc, err := w.parsed()
	if err != nil {
		return err
	}
	got, ok := lookup(doc, "nextCursor")
	if !ok {
		return fmt.Errorf("response has no nextCursor: %s", w.httpw.body)
	}
	cursor, isString := got.(string)
	if !isString || cursor == "" {
		return fmt.Errorf("response nextCursor = %v, want a non-empty string", got)
	}
	w.httpw.cursor = cursor
	return nil
}

func (w *world) getWithCursor(path string) error {
	if w.httpw == nil || w.httpw.cursor == "" {
		return fmt.Errorf("no cursor was remembered")
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return w.send(false, "GET", path+separator+"cursor="+url.QueryEscape(w.httpw.cursor), "", "", false)
}
