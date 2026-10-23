package features_test

import (
	"fmt"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
)

const defaultTransitMinutes = 120

func registerLaneSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^an open (GROUND|AIR) lane "([^"]*)" exists from "([^"]*)" to "([^"]*)"$`, w.givenLane)
	sc.Step(`^I open a (GROUND|AIR) lane "([^"]*)" from "([^"]*)" to "([^"]*)"$`, w.openLane)
	sc.Step(`^I open a (GROUND|AIR) lane "([^"]*)" from "([^"]*)" to "([^"]*)" with a transit of (-?\d+) minutes$`, w.openLaneWithTransit)
	sc.Step(`^I open a (GROUND|AIR) lane "([^"]*)" from "([^"]*)" to the node id "([^"]*)"$`, w.openLaneToID)
	sc.Step(`^I close the lane "([^"]*)"$`, w.closeLane)
	sc.Step(`^the lane "([^"]*)" is (OPEN|CLOSED)$`, w.laneStatusIs)
	sc.Step(`^listing the lanes from "([^"]*)" returns "([^"]*)"$`, w.listingLanesFrom)
}

func (w *world) openCommand(mode, origin, destination string, transit int) (usecases.OpenLaneCommand, error) {
	o, err := w.nodeID(origin)
	if err != nil {
		return usecases.OpenLaneCommand{}, err
	}
	d, err := w.nodeID(destination)
	if err != nil {
		return usecases.OpenLaneCommand{}, err
	}
	return usecases.OpenLaneCommand{OriginNodeID: o, DestinationNodeID: d, Mode: lane.Mode(mode), NominalTransitMinutes: transit}, nil
}

func (w *world) open(alias string, cmd usecases.OpenLaneCommand) error {
	v, err := usecases.NewOpenLane(w.deps).Execute(w.ctx(), cmd)
	if err == nil {
		w.lanes[alias] = lane.ID(v.LaneID)
	}
	return err
}

func (w *world) givenLane(mode, alias, origin, destination string) error {
	cmd, err := w.openCommand(mode, origin, destination, defaultTransitMinutes)
	if err != nil {
		return err
	}
	return w.given("open lane "+alias, func() error { return w.open(alias, cmd) })
}

func (w *world) openLane(mode, alias, origin, destination string) error {
	return w.openLaneWithTransit(mode, alias, origin, destination, defaultTransitMinutes)
}

func (w *world) openLaneWithTransit(mode, alias, origin, destination string, transit int) error {
	cmd, err := w.openCommand(mode, origin, destination, transit)
	if err != nil {
		return err
	}
	w.run(func() error { return w.open(alias, cmd) })
	return nil
}

func (w *world) openLaneToID(mode, alias, origin, destinationID string) error {
	o, err := w.nodeID(origin)
	if err != nil {
		return err
	}
	cmd := usecases.OpenLaneCommand{OriginNodeID: o, DestinationNodeID: node.ID(destinationID), Mode: lane.Mode(mode), NominalTransitMinutes: defaultTransitMinutes}
	w.run(func() error { return w.open(alias, cmd) })
	return nil
}

func (w *world) closeLane(alias string) error {
	id, err := w.laneID(alias)
	if err != nil {
		return err
	}
	w.run(func() error {
		_, err := usecases.NewCloseLane(w.deps).Execute(w.ctx(), id)
		return err
	})
	return nil
}

func (w *world) laneStatusIs(alias, status string) error {
	id, err := w.laneID(alias)
	if err != nil {
		return err
	}
	l, err := w.deps.Lanes.Get(w.ctx(), id)
	if err != nil {
		return err
	}
	if string(l.Status()) != status {
		return fmt.Errorf("lane %s is %s, want %s", alias, l.Status(), status)
	}
	return nil
}

func (w *world) aliasOfLane(id string) string {
	for alias, lid := range w.lanes {
		if string(lid) == id {
			return alias
		}
	}
	return id
}

func (w *world) listingLanesFrom(origin, want string) error {
	id, err := w.nodeID(origin)
	if err != nil {
		return err
	}
	got, err := usecases.NewListLanes(w.deps.Lanes).Execute(w.ctx(), usecases.ListLanesQuery{Origin: id})
	if err != nil {
		return err
	}
	out := ""
	for i, v := range got {
		if i > 0 {
			out += ", "
		}
		out += w.aliasOfLane(v.LaneID) + ":" + v.Status
	}
	if out != want {
		return fmt.Errorf("lanes from %s: %q, want %q", origin, out, want)
	}
	return nil
}
