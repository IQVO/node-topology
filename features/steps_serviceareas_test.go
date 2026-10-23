package features_test

import (
	"fmt"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

func registerServiceAreaSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the postal prefix "([^"]*)" is assigned to the station "([^"]*)" with assignment version (-?\d+)$`, w.givenAssignment)
	sc.Step(`^I assign the postal prefix "([^"]*)" to the station "([^"]*)" with assignment version (-?\d+)$`, w.assign)
	sc.Step(`^I assign the postal prefix "([^"]*)" to the node id "([^"]*)" with assignment version (-?\d+)$`, w.assignToID)
	sc.Step(`^the postal prefix "([^"]*)" resolves to the station "([^"]*)" in the region "([^"]*)" with assignment version (\d+)$`, w.resolvesTo)
	sc.Step(`^the postal prefix "([^"]*)" is not assigned$`, w.notAssigned)
}

func (w *world) assignCommand(prefix, station string, version int) (usecases.AssignServiceAreaCommand, error) {
	id, err := w.nodeID(station)
	if err != nil {
		return usecases.AssignServiceAreaCommand{}, err
	}
	return usecases.AssignServiceAreaCommand{PostalPrefix: servicearea.PostalPrefix(prefix), DeliveryStationNodeID: id, AssignmentVersion: version}, nil
}

func (w *world) givenAssignment(prefix, station string, version int) error {
	cmd, err := w.assignCommand(prefix, station, version)
	if err != nil {
		return err
	}
	return w.given("assign "+prefix, func() error {
		_, err := usecases.NewAssignServiceArea(w.deps).Execute(w.ctx(), cmd)
		return err
	})
}

func (w *world) assign(prefix, station string, version int) error {
	cmd, err := w.assignCommand(prefix, station, version)
	if err != nil {
		return err
	}
	w.run(func() error {
		_, err := usecases.NewAssignServiceArea(w.deps).Execute(w.ctx(), cmd)
		return err
	})
	return nil
}

func (w *world) assignToID(prefix, nodeID string, version int) error {
	cmd := usecases.AssignServiceAreaCommand{PostalPrefix: servicearea.PostalPrefix(prefix), AssignmentVersion: version}
	cmd.DeliveryStationNodeID = node.ID(nodeID)
	w.run(func() error {
		_, err := usecases.NewAssignServiceArea(w.deps).Execute(w.ctx(), cmd)
		return err
	})
	return nil
}

func (w *world) resolvesTo(prefix, station, regionCode string, version int) error {
	v, err := usecases.NewResolveServiceArea(w.deps.ServiceAreas).Execute(w.ctx(), servicearea.PostalPrefix(prefix))
	if err != nil {
		return err
	}
	sid, err := w.nodeID(station)
	if err != nil {
		return err
	}
	rid, err := w.regionID(regionCode)
	if err != nil {
		return err
	}
	if v.DeliveryStationNodeID != string(sid) || v.RegionID != string(rid) || v.AssignmentVersion != version {
		return fmt.Errorf("prefix %s resolves to %+v, want station %s region %s version %d", prefix, v, sid, rid, version)
	}
	return nil
}

func (w *world) notAssigned(prefix string) error {
	_, err := usecases.NewResolveServiceArea(w.deps.ServiceAreas).Execute(w.ctx(), servicearea.PostalPrefix(prefix))
	if err == nil {
		return fmt.Errorf("prefix %s is assigned", prefix)
	}
	return nil
}
