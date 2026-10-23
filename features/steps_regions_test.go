package features_test

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/region"
)

func registerRegionSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the region "([^"]*)" is defined$`, w.givenRegion)
	sc.Step(`^I define the region "([^"]*)" named "([^"]*)"$`, w.defineRegion)
	sc.Step(`^I read the region "([^"]*)"$`, w.readRegion)
	sc.Step(`^I read the region with the id "([^"]*)"$`, w.readRegionByID)
	sc.Step(`^the regions are listed as "([^"]*)"$`, w.regionsListedAs)
	sc.Step(`^the regions list is empty$`, w.regionsListEmpty)
	sc.Step(`^the region "([^"]*)" is named "([^"]*)"$`, w.regionNamed)
}

func (w *world) createRegion(code, name string) error {
	v, err := usecases.NewDefineRegion(w.deps).Execute(w.ctx(), usecases.DefineRegionCommand{Code: code, Name: name})
	if err == nil {
		w.regions[code] = region.ID(v.RegionID)
	}
	return err
}

func (w *world) givenRegion(code string) error {
	return w.given("define region "+code, func() error { return w.createRegion(code, "Region "+code) })
}

func (w *world) defineRegion(code, name string) error {
	w.run(func() error { return w.createRegion(code, name) })
	return nil
}

func (w *world) readRegion(code string) error {
	id, err := w.regionID(code)
	if err != nil {
		return err
	}
	return w.readRegionByID(string(id))
}

func (w *world) readRegionByID(id string) error {
	w.run(func() error {
		_, err := usecases.NewGetRegion(w.deps.Regions).Execute(w.ctx(), region.ID(id))
		return err
	})
	return nil
}

func (w *world) regionsListedAs(want string) error {
	got, err := usecases.NewListRegions(w.deps.Regions).Execute(w.ctx())
	if err != nil {
		return err
	}
	codes := make([]string, len(got))
	for i, r := range got {
		codes[i] = r.Code
	}
	if joined := strings.Join(codes, ", "); joined != want {
		return fmt.Errorf("regions listed as %q, want %q", joined, want)
	}
	return nil
}

func (w *world) regionsListEmpty() error { return w.regionsListedAs("") }

func (w *world) regionNamed(code, name string) error {
	id, err := w.regionID(code)
	if err != nil {
		return err
	}
	v, err := usecases.NewGetRegion(w.deps.Regions).Execute(w.ctx(), id)
	if err != nil {
		return err
	}
	if v.Name != name {
		return fmt.Errorf("region %s is named %q, want %q", code, v.Name, name)
	}
	return nil
}
