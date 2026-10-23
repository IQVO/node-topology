package features_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cucumber/godog"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// problems maps each problem slug of the REST contract (kebab-case of the
// model's Err name) to the error the use cases return for it.
var problems = map[string]error{
	"not-found":                          ports.ErrNotFound,
	"concurrent-update":                  ports.ErrConcurrentUpdate,
	"validation-failed":                  shared.ErrValidation,
	"illegal-transition":                 shared.ErrIllegalTransition,
	"operator-not-allowed-for-node-type": node.ErrOperatorNotAllowedForNodeType,
	"vendor-node-code-taken":             node.ErrVendorNodeCodeTaken,
	"stale-profile-revision":             node.ErrStaleProfileRevision,
	"incomplete-operational-profile":     node.ErrIncompleteOperationalProfile,
	"cutoff-outside-operating-hours":     node.ErrCutoffOutsideOperatingHours,
	"subtype-not-allowed":                node.ErrSubtypeNotAllowed,
	"sort-dwell-not-allowed":             node.ErrSortDwellNotAllowed,
	"node-retired":                       node.ErrNodeRetired,
	"lane-loop":                          lane.ErrLaneLoop,
	"lane-type-pair-not-allowed":         lane.ErrLaneTypePairNotAllowed,
	"lane-endpoint-not-active":           lane.ErrLaneEndpointNotActive,
	"duplicate-lane":                     lane.ErrDuplicateLane,
	"not-a-delivery-station":             servicearea.ErrNotADeliveryStation,
	"stale-assignment-version":           servicearea.ErrStaleAssignmentVersion,
	"region-code-taken":                  region.ErrRegionCodeTaken,
	"unknown-region":                     region.ErrUnknownRegion,
}

func registerOutcomeSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the command succeeds$`, w.commandSucceeds)
	sc.Step(`^the command is refused with "([^"]*)"$`, w.commandRefusedWith)
	sc.Step(`^the last command published the events? "([^"]*)"$`, w.publishedEvents)
	sc.Step(`^the last command published no events$`, w.publishedNothing)
	sc.Step(`^nothing was stored by the last command$`, w.nothingStored)
}

func (w *world) commandSucceeds() error {
	if !w.commandWasRun {
		return errNoCommand
	}
	if w.lastErr != nil {
		return fmt.Errorf("the command failed: %w", w.lastErr)
	}
	return nil
}

func (w *world) commandRefusedWith(slug string) error {
	want, known := problems[slug]
	if !known {
		return fmt.Errorf("unknown problem slug %q", slug)
	}
	if !w.commandWasRun {
		return errNoCommand
	}
	if !errors.Is(w.lastErr, want) {
		return fmt.Errorf("the command ended with %v, want %s", w.lastErr, slug)
	}
	return nil
}

func (w *world) publishedEvents(list string) error {
	want := strings.Split(list, ", ")
	if got := w.published(); !slices.Equal(got, want) {
		return fmt.Errorf("published %v, want %v", got, want)
	}
	return nil
}

func (w *world) publishedNothing() error {
	if got := w.published(); len(got) != 0 {
		return fmt.Errorf("published %v, want nothing", got)
	}
	return nil
}

func (w *world) nothingStored() error {
	if w.lastErr == nil {
		return errors.New("the last command succeeded, so something was stored")
	}
	if after := w.store.Counts(); after != w.countsBefore {
		return fmt.Errorf("the failed command changed the store: before %+v, after %+v", w.countsBefore, after)
	}
	return w.publishedNothing()
}
