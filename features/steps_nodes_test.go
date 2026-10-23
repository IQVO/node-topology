package features_test

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

func registerNodeSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a "([A-Z_]+)" node "([^"]*)" exists in the region "([^"]*)"$`, w.givenNode)
	sc.Step(`^an active "([A-Z_]+)" node "([^"]*)" exists in the region "([^"]*)"$`, w.givenActiveNode)
	sc.Step(`^I register a "([A-Z_]+)" node "([^"]*)" in the region "([^"]*)"$`, w.registerDefaultOperator)
	sc.Step(`^I register a "([A-Z_]+)" node "([^"]*)" operated by "(NETWORK|VENDOR)" in the region "([^"]*)"$`, w.registerOperatedBy)
	sc.Step(`^the vendor "([^"]*)" registers a "([A-Z_]+)" node "([^"]*)" with the vendor code "([^"]*)" in the region "([^"]*)"$`, w.registerVendorCode)
	sc.Step(`^I register a "([A-Z_]+)" node "([^"]*)" in an unknown region$`, w.registerUnknownRegion)
	sc.Step(`^I register a "([A-Z_]+)" node "([^"]*)" with the subtype "([A-Z_]+)" in the region "([^"]*)"$`, w.registerSubtype)
	sc.Step(`^I activate the node "([^"]*)"$`, w.activate)
	sc.Step(`^I suspend the node "([^"]*)" because "([^"]*)"$`, w.suspend)
	sc.Step(`^I retire the node "([^"]*)"$`, w.retire)
	sc.Step(`^the node "([^"]*)" is (PROPOSED|ACTIVE|SUSPENDED|RETIRED)$`, w.nodeStatusIs)
	sc.Step(`^the node "([^"]*)" is run by "(NETWORK|VENDOR)"$`, w.nodeRunBy)
	sc.Step(`^the node "([^"]*)" has the capability "([a-zA-Z]+)"$`, w.nodeHasCapability)
	sc.Step(`^the node "([^"]*)" lacks the capability "([a-zA-Z]+)"$`, w.nodeLacksCapability)
	sc.Step(`^the node "([^"]*)" is suspended because "([^"]*)"$`, w.nodeSuspendedBecause)
	sc.Step(`^listing nodes with the type "([A-Z_]*)", status "([A-Z_]*)" and region "([^"]*)" returns "([^"]*)"$`, w.listingReturns)
	sc.Step(`^listing nodes with the type "([A-Z_]*)" fails as invalid$`, w.listingInvalidType)
}

// register runs RegisterNode and remembers the alias of the new node.
func (w *world) register(alias string, cmd usecases.RegisterNodeCommand) error {
	v, err := usecases.NewRegisterNode(w.deps).Execute(w.ctx(), cmd)
	if err == nil {
		w.nodes[alias] = node.ID(v.NodeID)
	}
	return err
}

// command builds a registration in the named region.
func (w *world) command(typ, regionCode string, operator node.Operator, vendor *node.VendorRef) (usecases.RegisterNodeCommand, error) {
	rid, err := w.regionID(regionCode)
	if err != nil {
		return usecases.RegisterNodeCommand{}, err
	}
	return usecases.RegisterNodeCommand{
		Type: node.Type(typ), Operator: operator, Vendor: vendor, RegionID: rid, Timezone: "America/Sao_Paulo",
		Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	}, nil
}

// defaultCommand picks the operator the type allows: a type a vendor may run
// is VENDOR-operated under vendor-1 with the alias as its code.
func (w *world) defaultCommand(typ, alias, regionCode string) (usecases.RegisterNodeCommand, error) {
	if node.Type(typ).AllowsOperator(node.OperatorVendor) {
		return w.command(typ, regionCode, node.OperatorVendor, &node.VendorRef{VendorID: "vendor-1", VendorNodeCode: alias})
	}
	return w.command(typ, regionCode, node.OperatorNetwork, nil)
}

func (w *world) givenNode(typ, alias, regionCode string) error {
	return w.given("register node "+alias, func() error {
		cmd, err := w.defaultCommand(typ, alias, regionCode)
		if err != nil {
			return err
		}
		return w.register(alias, cmd)
	})
}

func (w *world) givenActiveNode(typ, alias, regionCode string) error {
	if err := w.givenNode(typ, alias, regionCode); err != nil {
		return err
	}
	return w.given("activate node "+alias, func() error {
		id := w.nodes[alias]
		d := repocontract.Declaration(1)
		if _, err := usecases.NewDeclareOperationalProfile(w.deps).Execute(w.ctx(), usecases.DeclareOperationalProfileCommand{NodeID: id, Declaration: d}); err != nil {
			return err
		}
		_, err := usecases.NewActivateNode(w.deps).Execute(w.ctx(), id)
		return err
	})
}

func (w *world) registerWith(alias string, build func() (usecases.RegisterNodeCommand, error)) error {
	cmd, err := build()
	if err != nil {
		return err
	}
	w.run(func() error { return w.register(alias, cmd) })
	return nil
}

func (w *world) registerDefaultOperator(typ, alias, regionCode string) error {
	return w.registerWith(alias, func() (usecases.RegisterNodeCommand, error) { return w.defaultCommand(typ, alias, regionCode) })
}

func (w *world) registerOperatedBy(typ, alias, operator, regionCode string) error {
	return w.registerWith(alias, func() (usecases.RegisterNodeCommand, error) {
		if operator == string(node.OperatorVendor) {
			return w.command(typ, regionCode, node.OperatorVendor, &node.VendorRef{VendorID: "vendor-1", VendorNodeCode: alias})
		}
		return w.command(typ, regionCode, node.OperatorNetwork, nil)
	})
}

func (w *world) registerVendorCode(vendor, typ, alias, code, regionCode string) error {
	return w.registerWith(alias, func() (usecases.RegisterNodeCommand, error) {
		return w.command(typ, regionCode, node.OperatorVendor, &node.VendorRef{VendorID: vendor, VendorNodeCode: code})
	})
}

func (w *world) registerUnknownRegion(typ, alias string) error {
	cmd, err := w.defaultCommandIn(typ, alias, "region-unknown")
	if err != nil {
		return err
	}
	w.run(func() error { return w.register(alias, cmd) })
	return nil
}

// defaultCommandIn is defaultCommand for a region id the scenario never defined.
func (w *world) defaultCommandIn(typ, alias string, regionID region.ID) (usecases.RegisterNodeCommand, error) {
	const code = "unknown"
	w.regions[code] = regionID
	return w.defaultCommand(typ, alias, code)
}

func (w *world) registerSubtype(typ, alias, subtype, regionCode string) error {
	return w.registerWith(alias, func() (usecases.RegisterNodeCommand, error) {
		cmd, err := w.defaultCommand(typ, alias, regionCode)
		cmd.Subtype = node.Subtype(subtype)
		return cmd, err
	})
}

// onNode runs a node command by alias.
func (w *world) onNode(alias string, do func(id node.ID) error) error {
	id, err := w.nodeID(alias)
	if err != nil {
		return err
	}
	w.run(func() error { return do(id) })
	return nil
}

func (w *world) activate(alias string) error {
	return w.onNode(alias, func(id node.ID) error {
		_, err := usecases.NewActivateNode(w.deps).Execute(w.ctx(), id)
		return err
	})
}

func (w *world) suspend(alias, reason string) error {
	return w.onNode(alias, func(id node.ID) error {
		_, err := usecases.NewSuspendNode(w.deps).Execute(w.ctx(), id, reason)
		return err
	})
}

func (w *world) retire(alias string) error {
	return w.onNode(alias, func(id node.ID) error {
		_, err := usecases.NewRetireNode(w.deps).Execute(w.ctx(), id)
		return err
	})
}

// nodeView reads a node by alias through the GetNode query.
func (w *world) nodeView(alias string) (views.Node, error) {
	id, err := w.nodeID(alias)
	if err != nil {
		return views.Node{}, err
	}
	return usecases.NewGetNode(w.deps.Nodes).Execute(w.ctx(), id)
}

func (w *world) nodeStatusIs(alias, status string) error {
	v, err := w.nodeView(alias)
	if err != nil {
		return err
	}
	if v.Status != status {
		return fmt.Errorf("node %s is %s, want %s", alias, v.Status, status)
	}
	return nil
}

func (w *world) nodeRunBy(alias, operator string) error {
	v, err := w.nodeView(alias)
	if err != nil {
		return err
	}
	if v.Operator != operator {
		return fmt.Errorf("node %s is run by %s, want %s", alias, v.Operator, operator)
	}
	return nil
}

func capability(c views.Capabilities, name string) (bool, error) {
	switch name {
	case "outbound":
		return c.Outbound, nil
	case "inbound":
		return c.Inbound, nil
	case "returns":
		return c.Returns, nil
	case "sort":
		return c.Sort, nil
	case "lastMile":
		return c.LastMile, nil
	case "air":
		return c.Air, nil
	}
	return false, fmt.Errorf("unknown capability %q", name)
}

func (w *world) hasCapability(alias, name string, want bool) error {
	v, err := w.nodeView(alias)
	if err != nil {
		return err
	}
	got, err := capability(v.Capabilities, name)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("node %s capability %s = %v, want %v", alias, name, got, want)
	}
	return nil
}

func (w *world) nodeHasCapability(alias, name string) error {
	return w.hasCapability(alias, name, true)
}
func (w *world) nodeLacksCapability(alias, name string) error {
	return w.hasCapability(alias, name, false)
}

func (w *world) nodeSuspendedBecause(alias, reason string) error {
	v, err := w.nodeView(alias)
	if err != nil {
		return err
	}
	if v.Status != "SUSPENDED" || v.SuspendReason != reason {
		return fmt.Errorf("node %s is %s with reason %q, want SUSPENDED because %q", alias, v.Status, v.SuspendReason, reason)
	}
	return nil
}

// aliasOf maps a node id back to the alias the scenario gave it.
func (w *world) aliasOf(id string) string {
	for alias, nid := range w.nodes {
		if string(nid) == id {
			return alias
		}
	}
	return id
}

func (w *world) listingReturns(typ, status, regionCode, want string) error {
	q := usecases.ListNodesQuery{Type: node.Type(typ), Status: node.Status(status)}
	if regionCode != "" {
		rid, err := w.regionID(regionCode)
		if err != nil {
			return err
		}
		q.RegionID = rid
	}
	got, err := usecases.NewListNodes(w.deps.Nodes).Execute(w.ctx(), q)
	if err != nil {
		return err
	}
	aliases := []string{}
	for _, v := range got {
		aliases = append(aliases, w.aliasOf(v.NodeID))
	}
	slices.Sort(aliases)
	if joined := strings.Join(aliases, ", "); joined != want {
		return fmt.Errorf("listing returned %q, want %q", joined, want)
	}
	return nil
}

func (w *world) listingInvalidType(typ string) error {
	w.run(func() error {
		_, err := usecases.NewListNodes(w.deps.Nodes).Execute(w.ctx(), usecases.ListNodesQuery{Type: node.Type(typ)})
		return err
	})
	return nil
}
