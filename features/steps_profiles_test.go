package features_test

import (
	"fmt"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
)

func registerProfileSteps(sc *godog.ScenarioContext, w *world) {
	registerInboundSteps(sc, w)
	sc.Step(`^the node "([^"]*)" has declared a complete profile$`, w.givenCompleteProfile)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with (\d+) minutes of handling and (\d+) cutoffs?$`, w.declareRevision)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with a Monday cutoff at "([^"]*)"$`, w.declareWithCutoffAt)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with a sort dwell of (\d+) minutes$`, w.declareWithSortDwell)
	sc.Step(`^the node "([^"]*)" has profile revision (\d+)$`, w.hasProfileRevision)
	sc.Step(`^the node "([^"]*)" has no profile$`, w.hasNoProfile)
	sc.Step(`^the node "([^"]*)" keeps (\d+) stored profile revisions$`, w.keepsStoredRevisions)
	sc.Step(`^revision (\d+) of the node "([^"]*)" still has (\d+) minutes of handling$`, w.revisionHandling)
}

// declaration is a valid revision: Monday 06:00-22:00, a 14:00 Monday cutoff
// (or none), the given handling time.
func declaration(revision, handling, cutoffs int) node.ProfileDeclaration {
	d := repocontract.Declaration(revision)
	d.HandlingTimeMinutes = handling
	if cutoffs == 0 {
		d.Cutoffs = nil
	}
	return d
}

func (w *world) declare(alias string, d node.ProfileDeclaration) error {
	id, err := w.nodeID(alias)
	if err != nil {
		return err
	}
	_, err = usecases.NewDeclareOperationalProfile(w.deps).Execute(w.ctx(), usecases.DeclareOperationalProfileCommand{NodeID: id, Declaration: d})
	return err
}

func (w *world) givenCompleteProfile(alias string) error {
	return w.given("declare a complete profile of "+alias, func() error { return w.declare(alias, declaration(1, 30, 1)) })
}

func (w *world) declareAndRemember(alias string, d node.ProfileDeclaration) error {
	if _, err := w.nodeID(alias); err != nil {
		return err
	}
	w.run(func() error { return w.declare(alias, d) })
	return nil
}

func (w *world) declareRevision(revision int, alias string, handling, cutoffs int) error {
	return w.declareAndRemember(alias, declaration(revision, handling, cutoffs))
}

func (w *world) declareWithCutoffAt(revision int, alias, at string) error {
	d := declaration(revision, 30, 1)
	d.Cutoffs = []node.CutoffSlot{{LocalTime: node.TimeOfDay(at), Days: []node.Weekday{node.Monday}}}
	return w.declareAndRemember(alias, d)
}

func (w *world) declareWithSortDwell(revision int, alias string, minutes int) error {
	d := declaration(revision, 30, 1)
	d.SortDwellMinutes = &minutes
	return w.declareAndRemember(alias, d)
}

func (w *world) hasProfileRevision(alias string, want int) error {
	v, err := w.nodeView(alias)
	if err != nil {
		return err
	}
	if v.OperationalProfile == nil || v.OperationalProfile.ProfileRevision != want {
		return fmt.Errorf("node %s profile = %+v, want revision %d", alias, v.OperationalProfile, want)
	}
	return nil
}

func (w *world) hasNoProfile(alias string) error {
	v, err := w.nodeView(alias)
	if err != nil {
		return err
	}
	if v.OperationalProfile != nil {
		return fmt.Errorf("node %s has profile revision %d, want none", alias, v.OperationalProfile.ProfileRevision)
	}
	return nil
}

func (w *world) storedRevisions(alias string) ([]node.OperationalProfile, error) {
	id, err := w.nodeID(alias)
	if err != nil {
		return nil, err
	}
	return w.deps.Nodes.ProfileRevisions(w.ctx(), id)
}

func (w *world) keepsStoredRevisions(alias string, want int) error {
	rows, err := w.storedRevisions(alias)
	if err != nil {
		return err
	}
	if len(rows) != want {
		return fmt.Errorf("node %s keeps %d profile revisions, want %d", alias, len(rows), want)
	}
	return nil
}

func (w *world) revisionHandling(revision int, alias string, want int) error {
	rows, err := w.storedRevisions(alias)
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.Revision == revision {
			if p.HandlingTimeMinutes != want {
				return fmt.Errorf("revision %d of %s has %d minutes of handling, want %d", revision, alias, p.HandlingTimeMinutes, want)
			}
			return nil
		}
	}
	return fmt.Errorf("revision %d of %s is not stored", revision, alias)
}

func registerInboundSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with the inbound limits (-?\d+) g, (-?\d+) x (-?\d+) x (-?\d+) mm, pallets accepted and SSCC required$`, w.declareWithInbound)
	sc.Step(`^I declare revision (\d+) of the node "([^"]*)" with an empty inbound declaration$`, w.declareWithEmptyInbound)
	sc.Step(`^the published profile carries the inbound limits (\d+) g and (\d+) x (\d+) x (\d+) mm$`, w.publishedInbound)
	sc.Step(`^the published profile carries no inbound limits$`, w.publishedNoInbound)
	sc.Step(`^the node "([^"]*)" shows the inbound limits (\d+) g and (\d+) x (\d+) x (\d+) mm$`, w.nodeInbound)
	sc.Step(`^the node "([^"]*)" shows no inbound limits$`, w.nodeNoInbound)
	sc.Step(`^revision (\d+) of the node "([^"]*)" still has the inbound limits (\d+) g and (\d+) x (\d+) x (\d+) mm$`, w.revisionInbound)
}

func (w *world) declareWithInbound(revision int, alias string, weight, l, wd, h int) error {
	yes := true
	d := declaration(revision, 30, 1)
	d.Inbound = &node.InboundLimits{
		MaxCartonWeightG: &weight, MaxCartonDimsMm: &node.CartonDims{L: l, W: wd, H: h}, AcceptsPallets: &yes, RequiresSscc: &yes,
	}
	return w.declareAndRemember(alias, d)
}

func (w *world) declareWithEmptyInbound(revision int, alias string) error {
	d := declaration(revision, 30, 1)
	d.Inbound = &node.InboundLimits{}
	return w.declareAndRemember(alias, d)
}

func wantInbound(got *node.InboundLimits, weight, l, wd, h int) error {
	if got == nil || got.MaxCartonWeightG == nil || got.MaxCartonDimsMm == nil {
		return fmt.Errorf("inbound limits = %+v, want %d g and %d x %d x %d mm", got, weight, l, wd, h)
	}
	if *got.MaxCartonWeightG != weight || *got.MaxCartonDimsMm != (node.CartonDims{L: l, W: wd, H: h}) {
		return fmt.Errorf("inbound limits = %d g, %+v, want %d g and %d x %d x %d mm", *got.MaxCartonWeightG, *got.MaxCartonDimsMm, weight, l, wd, h)
	}
	return nil
}

func (w *world) publishedProfile() (node.NodeOperationalProfileDeclared, error) {
	events := w.store.Outbox().Events()[w.outboxBefore:]
	if len(events) != 1 {
		return node.NodeOperationalProfileDeclared{}, fmt.Errorf("the last command published %d events, want 1", len(events))
	}
	e, ok := events[0].(node.NodeOperationalProfileDeclared)
	if !ok {
		return e, fmt.Errorf("published %T, want NodeOperationalProfileDeclared", events[0])
	}
	return e, nil
}

func (w *world) publishedInbound(weight, l, wd, h int) error {
	e, err := w.publishedProfile()
	if err != nil {
		return err
	}
	return wantInbound(e.Inbound, weight, l, wd, h)
}

func (w *world) publishedNoInbound() error {
	e, err := w.publishedProfile()
	if err != nil {
		return err
	}
	if e.Inbound != nil {
		return fmt.Errorf("the event carries inbound limits %+v, want none", e.Inbound)
	}
	return nil
}

func (w *world) nodeProfile(alias string) (*node.InboundLimits, error) {
	id, err := w.nodeID(alias)
	if err != nil {
		return nil, err
	}
	n, err := w.deps.Nodes.Get(w.ctx(), id)
	if err != nil {
		return nil, err
	}
	p, _ := n.Profile()
	return p.Inbound, nil
}

func (w *world) nodeInbound(alias string, weight, l, wd, h int) error {
	got, err := w.nodeProfile(alias)
	if err != nil {
		return err
	}
	return wantInbound(got, weight, l, wd, h)
}

func (w *world) nodeNoInbound(alias string) error {
	got, err := w.nodeProfile(alias)
	if err != nil {
		return err
	}
	if got != nil {
		return fmt.Errorf("node %s shows inbound limits %+v, want none", alias, got)
	}
	return nil
}

func (w *world) revisionInbound(revision int, alias string, weight, l, wd, h int) error {
	id, err := w.nodeID(alias)
	if err != nil {
		return err
	}
	rows, err := w.deps.Nodes.ProfileRevisions(w.ctx(), id)
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.Revision == revision {
			return wantInbound(p.Inbound, weight, l, wd, h)
		}
	}
	return fmt.Errorf("node %s has no stored revision %d", alias, revision)
}
