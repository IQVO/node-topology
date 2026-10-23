package http_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"

	inboundhttp "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// failing is a use case that always fails with err, one Execute per signature
// the handlers call.
type failing struct{ err error }

type (
	failRegister struct{ failing }
	failDeclare  struct{ failing }
	failNodeID   struct{ failing }
	failSuspend  struct{ failing }
	failListN    struct{ failing }
	failOpen     struct{ failing }
	failClose    struct{ failing }
	failListL    struct{ failing }
	failAssign   struct{ failing }
	failResolve  struct{ failing }
	failDefine   struct{ failing }
	failGetReg   struct{ failing }
	failListReg  struct{ failing }
)

func (f failRegister) Execute(context.Context, usecases.RegisterNodeCommand) (views.Node, error) {
	return views.Node{}, f.err
}

func (f failDeclare) Execute(context.Context, usecases.DeclareOperationalProfileCommand) (views.Node, error) {
	return views.Node{}, f.err
}
func (f failNodeID) Execute(context.Context, node.ID) (views.Node, error) { return views.Node{}, f.err }
func (f failSuspend) Execute(context.Context, node.ID, string) (views.Node, error) {
	return views.Node{}, f.err
}
func (f failListN) Execute(context.Context, usecases.ListNodesQuery) ([]views.Node, error) {
	return nil, f.err
}
func (f failOpen) Execute(context.Context, usecases.OpenLaneCommand) (views.Lane, error) {
	return views.Lane{}, f.err
}
func (f failClose) Execute(context.Context, lane.ID) (views.Lane, error) { return views.Lane{}, f.err }
func (f failListL) Execute(context.Context, usecases.ListLanesQuery) ([]views.Lane, error) {
	return nil, f.err
}
func (f failAssign) Execute(context.Context, usecases.AssignServiceAreaCommand) (views.ServiceArea, error) {
	return views.ServiceArea{}, f.err
}
func (f failResolve) Execute(context.Context, servicearea.PostalPrefix) (views.ServiceArea, error) {
	return views.ServiceArea{}, f.err
}
func (f failDefine) Execute(context.Context, usecases.DefineRegionCommand) (views.Region, error) {
	return views.Region{}, f.err
}
func (f failGetReg) Execute(context.Context, region.ID) (views.Region, error) {
	return views.Region{}, f.err
}
func (f failListReg) Execute(context.Context) ([]views.Region, error) { return nil, f.err }

func failingServer(err error) *inboundhttp.Server {
	f := failing{err}
	return &inboundhttp.Server{
		RegisterNode: failRegister{f}, DeclareProfile: failDeclare{f}, ActivateNode: failNodeID{f}, SuspendNode: failSuspend{f},
		RetireNode: failNodeID{f}, GetNode: failNodeID{f}, ListNodes: failListN{f}, OpenLane: failOpen{f}, CloseLane: failClose{f},
		ListLanes: failListL{f}, AssignServiceArea: failAssign{f}, ResolveServiceArea: failResolve{f}, DefineRegion: failDefine{f},
		GetRegion: failGetReg{f}, ListRegions: failListReg{f},
		Idempotency: requireKeyOnly, Readiness: inboundhttp.NewReadiness(0, 0),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// every operation of apis/openapi.yaml, with a body that reaches the use case.
var operations = []struct {
	name, method, path, body string
}{
	{"registerNode", http.MethodPost, "/nodes", registerBodyMin},
	{"listNodes", http.MethodGet, "/nodes", ""},
	{"getNode", http.MethodGet, "/nodes/node-1", ""},
	{"declareOperationalProfile", http.MethodPut, "/nodes/node-1/operational-profile", `{"profileRevision":1}`},
	{"activateNode", http.MethodPost, "/nodes/node-1:activate", ""},
	{"suspendNode", http.MethodPost, "/nodes/node-1:suspend", `{"reason":"r"}`},
	{"retireNode", http.MethodPost, "/nodes/node-1:retire", ""},
	{"openLane", http.MethodPost, "/lanes", `{"originNodeId":"a","destinationNodeId":"b","mode":"GROUND","nominalTransitMinutes":5}`},
	{"listLanes", http.MethodGet, "/lanes", ""},
	{"closeLane", http.MethodPost, "/lanes/lane-1:close", ""},
	{"assignServiceArea", http.MethodPut, "/service-areas/01310", `{"deliveryStationNodeId":"n","assignmentVersion":1}`},
	{"resolveServiceArea", http.MethodGet, "/service-areas/01310", ""},
	{"defineRegion", http.MethodPost, "/regions", `{"code":"SE","name":"Southeast"}`},
	{"listRegions", http.MethodGet, "/regions", ""},
	{"getRegion", http.MethodGet, "/regions/region-1", ""},
}

const registerBodyMin = `{"nodeType":"SORTATION_CENTER","operator":"NETWORK","regionId":"r","timezone":"UTC","location":{"postalCode":"1","countryCode":"BR"}}`

// the error catalogue the contract promises: every model Err plus the shared
// types, with the status and slug apis/openapi.yaml documents.
var catalogue = []struct {
	err    error
	status int
	slug   string
}{
	{ports.ErrNotFound, 404, "not-found"},
	{shared.ErrIllegalTransition, 409, "illegal-transition"},
	{ports.ErrConcurrentUpdate, 409, "concurrent-update"},
	{shared.ErrValidation, 422, "validation-failed"},
	{node.ErrVendorNodeCodeTaken, 409, "vendor-node-code-taken"},
	{node.ErrStaleProfileRevision, 409, "stale-profile-revision"},
	{node.ErrIncompleteOperationalProfile, 409, "incomplete-operational-profile"},
	{node.ErrNodeRetired, 409, "node-retired"},
	{node.ErrOperatorNotAllowedForNodeType, 422, "operator-not-allowed-for-node-type"},
	{node.ErrSubtypeNotAllowed, 422, "subtype-not-allowed"},
	{node.ErrCutoffOutsideOperatingHours, 422, "cutoff-outside-operating-hours"},
	{node.ErrSortDwellNotAllowed, 422, "sort-dwell-not-allowed"},
	{lane.ErrLaneEndpointNotActive, 409, "lane-endpoint-not-active"},
	{lane.ErrDuplicateLane, 409, "duplicate-lane"},
	{lane.ErrLaneLoop, 422, "lane-loop"},
	{lane.ErrLaneTypePairNotAllowed, 422, "lane-type-pair-not-allowed"},
	{servicearea.ErrNotADeliveryStation, 409, "not-a-delivery-station"},
	{servicearea.ErrStaleAssignmentVersion, 409, "stale-assignment-version"},
	{region.ErrRegionCodeTaken, 409, "region-code-taken"},
	{region.ErrUnknownRegion, 422, "unknown-region"},
}

// TestEveryOperationMapsEveryError sends every operation to a server whose use
// cases fail with each catalogued error (wrapped, as the use cases wrap them)
// and checks the status and the problem slug.
func TestEveryOperationMapsEveryError(t *testing.T) {
	for _, c := range catalogue {
		t.Run(c.slug, func(t *testing.T) {
			a := &api{t: t, h: routerOf(t, failingServer(fmt.Errorf("use case: %w", c.err)))}
			for _, op := range operations {
				var r reply
				if op.method == http.MethodGet {
					r = a.get(op.path)
				} else {
					r = a.keyed(op.method, op.path, op.body)
				}
				if r.status != c.status || r.slug() != c.slug || !r.isProblem() {
					t.Errorf("%s: %d %q problem=%v, want %d %q", op.name, r.status, r.slug(), r.isProblem(), c.status, c.slug)
				}
			}
		})
	}
}

// TestAnUnknownErrorIsAnOpaque500 never leaks the internal message.
func TestAnUnknownErrorIsAnOpaque500(t *testing.T) {
	a := &api{t: t, h: routerOf(t, failingServer(errors.New("pq: password authentication failed")))}
	for _, op := range operations {
		var r reply
		if op.method == http.MethodGet {
			r = a.get(op.path)
		} else {
			r = a.keyed(op.method, op.path, op.body)
		}
		if r.status != 500 || r.slug() != "internal-error" || strings.Contains(r.raw, "password") {
			t.Errorf("%s: %d %s, want an opaque 500 internal-error", op.name, r.status, r.raw)
		}
	}
}

var kebab = regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)

// TestEveryModelErrHasItsOwnKebabSlug: one slug per model Err, the kebab-case
// of the Err name without the Err prefix.
func TestEveryModelErrHasItsOwnKebabSlug(t *testing.T) {
	modelErrs := map[string]error{
		"ErrVendorNodeCodeTaken": node.ErrVendorNodeCodeTaken, "ErrStaleProfileRevision": node.ErrStaleProfileRevision,
		"ErrIncompleteOperationalProfile": node.ErrIncompleteOperationalProfile, "ErrNodeRetired": node.ErrNodeRetired,
		"ErrOperatorNotAllowedForNodeType": node.ErrOperatorNotAllowedForNodeType, "ErrSubtypeNotAllowed": node.ErrSubtypeNotAllowed,
		"ErrCutoffOutsideOperatingHours": node.ErrCutoffOutsideOperatingHours, "ErrSortDwellNotAllowed": node.ErrSortDwellNotAllowed,
		"ErrLaneEndpointNotActive": lane.ErrLaneEndpointNotActive, "ErrDuplicateLane": lane.ErrDuplicateLane,
		"ErrLaneLoop": lane.ErrLaneLoop, "ErrLaneTypePairNotAllowed": lane.ErrLaneTypePairNotAllowed,
		"ErrNotADeliveryStation": servicearea.ErrNotADeliveryStation, "ErrStaleAssignmentVersion": servicearea.ErrStaleAssignmentVersion,
		"ErrRegionCodeTaken": region.ErrRegionCodeTaken, "ErrUnknownRegion": region.ErrUnknownRegion,
	}
	seen := map[string]string{}
	for name, err := range modelErrs {
		want := kebabOf(strings.TrimPrefix(name, "Err"))
		var got string
		for _, c := range catalogue {
			if errors.Is(err, c.err) {
				got = c.slug
			}
		}
		if got != want || !kebab.MatchString(got) {
			t.Errorf("%s has slug %q, want %q", name, got, want)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%s and %s share the slug %q", name, prev, got)
		}
		seen[got] = name
	}
	if len(modelErrs) != 16 {
		t.Fatalf("the model has 16 Errs, the test lists %d", len(modelErrs))
	}
}

func kebabOf(pascal string) string {
	var b strings.Builder
	for i, r := range pascal {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
