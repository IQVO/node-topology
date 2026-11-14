package features_test

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

func registerServiceAreaSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the postal prefix "([^"]*)" is assigned to the station "([^"]*)" with assignment version (-?\d+)$`, w.givenAssignment)
	sc.Step(`^I assign the postal prefix "([^"]*)" to the station "([^"]*)" with assignment version (-?\d+)$`, w.assign)
	sc.Step(`^I assign the postal prefix "([^"]*)" to the node id "([^"]*)" with assignment version (-?\d+)$`, w.assignToID)
	sc.Step(`^the postal prefix "([^"]*)" resolves to the station "([^"]*)" in the region "([^"]*)" with assignment version (\d+)$`, w.resolvesTo)
	sc.Step(`^the postal prefix "([^"]*)" is not assigned$`, w.notAssigned)
	sc.Step(`^I assign the postal prefix "([^\"]*)" to the station "([^\"]*)" with assignment version (\d+) and delivery mode "([^\"]*)" and carriers "([^\"]*)"$`, w.assignWithMode)
	sc.Step(`^the postal prefix "([^\"]*)" resolves in mode "([^\"]*)" with the carriers "([^\"]*)"$`, w.resolvesInMode)
	sc.Step(`^the postal prefix "([^\"]*)" resolves with no delivery mode$`, w.resolvesWithNoMode)
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

// parseCarrierSpec reads the scenario's carrier list, one option per comma:
// "carrier-a@sc-1:320:95" is carrierId@nodeAlias:costCents:transitMinutes.
func (w *world) parseCarrierSpec(spec string) ([]usecases.CarrierOptionCommand, error) {
	if spec == "" {
		return nil, nil
	}
	out := []usecases.CarrierOptionCommand{}
	for _, part := range strings.Split(spec, ",") {
		idAt := strings.SplitN(part, "@", 2)
		bits := strings.Split(idAt[1], ":")
		if len(idAt) != 2 || len(bits) != 3 {
			return nil, fmt.Errorf("carrier spec %q is not carrier@node:cost:transit", part)
		}
		id, err := w.nodeID(bits[0])
		if err != nil {
			// not an alias of this scenario: a raw node id (the
			// unknown-handoff-node scenario names a node that was
			// never created).
			id = node.ID(bits[0])
		}
		cost, err := strconv.ParseInt(bits[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("carrier cost in %q: %w", part, err)
		}
		transit, err := strconv.Atoi(bits[2])
		if err != nil {
			return nil, fmt.Errorf("carrier transit in %q: %w", part, err)
		}
		out = append(out, usecases.CarrierOptionCommand{CarrierID: idAt[0], HandoffNodeID: id, CostPerPackageCents: cost, TransitMinutes: transit})
	}
	return out, nil
}

// renderCarrierSpec renders the view back into the scenario's carrier syntax.
func (w *world) renderCarrierSpec(options []views.CarrierOption) (string, error) {
	if len(options) == 0 {
		return "", nil
	}
	parts := make([]string, len(options))
	for i, o := range options {
		alias, err := w.nodeAlias(node.ID(o.HandoffNodeID))
		if err != nil {
			return "", err
		}
		parts[i] = fmt.Sprintf("%s@%s:%d:%d", o.CarrierID, alias, o.CostPerPackageCents, o.TransitMinutes)
	}
	return strings.Join(parts, ","), nil
}

// nodeAlias is the reverse of nodeID: the scenario alias of a node id.
func (w *world) nodeAlias(id node.ID) (string, error) {
	for alias, got := range w.nodes {
		if got == id {
			return alias, nil
		}
	}
	return "", fmt.Errorf("no scenario alias for node %q", id)
}

func (w *world) assignWithMode(prefix, station string, version int, mode, carriers string) error {
	id, err := w.nodeID(station)
	if err != nil {
		return err
	}
	options, err := w.parseCarrierSpec(carriers)
	if err != nil {
		return err
	}
	cmd := usecases.AssignServiceAreaCommand{
		PostalPrefix: servicearea.PostalPrefix(prefix), DeliveryStationNodeID: id, AssignmentVersion: version,
		DeliveryMode: servicearea.DeliveryMode(mode), Carriers: options,
	}
	w.run(func() error {
		_, err := usecases.NewAssignServiceArea(w.deps).Execute(w.ctx(), cmd)
		return err
	})
	return nil
}

func (w *world) resolvesInMode(prefix, mode, carriers string) error {
	v, err := usecases.NewResolveServiceArea(w.deps.ServiceAreas).Execute(w.ctx(), servicearea.PostalPrefix(prefix))
	if err != nil {
		return err
	}
	got, err := w.renderCarrierSpec(v.Carriers)
	if err != nil {
		return err
	}
	if v.DeliveryMode != mode || got != carriers {
		return fmt.Errorf("prefix %s resolves to mode %q carriers %q, want %q %q", prefix, v.DeliveryMode, got, mode, carriers)
	}
	return nil
}

func (w *world) resolvesWithNoMode(prefix string) error {
	v, err := usecases.NewResolveServiceArea(w.deps.ServiceAreas).Execute(w.ctx(), servicearea.PostalPrefix(prefix))
	if err != nil {
		return err
	}
	if v.DeliveryMode != "" || v.Carriers != nil {
		return fmt.Errorf("prefix %s carries mode %q carriers %+v, want none", prefix, v.DeliveryMode, v.Carriers)
	}
	return nil
}
