package a2a_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	inbounda2a "github.com/claudioed/node-topology/internal/adapters/inbound/a2a"
	"github.com/claudioed/node-topology/internal/adapters/outbound/memory"
	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// world is the seeded topology every skill test reads: two regions, three
// ACTIVE nodes in the first (a vendor-run fulfillment centre with a declared
// profile, a network sortation centre, a network delivery station that serves
// postal prefix 013) and one PROPOSED delivery station in the second.
type world struct {
	fcID, scID, dsID, proposedID, northID, southID string
}

// harness is the real A2A handler over in-memory repositories, exactly as
// cmd/a2a wires the use cases (minus Postgres). The write use cases exist ONLY
// here, to seed data the way the REST adapter would: the A2A surface has none.
type harness struct {
	store *memory.Store
	deps  usecases.Deps
	w     world
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store := memory.NewStore()
	deps := usecases.Deps{
		Nodes: store.Nodes(), Lanes: store.Lanes(), ServiceAreas: store.ServiceAreas(), Regions: store.Regions(),
		Outbox: store.Outbox(), UoW: store.UnitOfWork(), Clock: memory.NewClock(now), IDs: &memory.SequentialIDs{},
	}
	h := &harness{store: store, deps: deps}
	h.seed(t)
	return h
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (h *harness) register(t *testing.T, typ node.Type, operator node.Operator, regionID, postal string) string {
	t.Helper()
	cmd := usecases.RegisterNodeCommand{
		Type: typ, Operator: operator, RegionID: region.ID(regionID), Timezone: "America/Sao_Paulo",
		Location: node.GeoLocation{PostalCode: postal, CountryCode: "BR"},
	}
	if typ == node.FulfillmentCenter {
		cmd.Subtype = node.SubtypeSortable
		cmd.Vendor = &node.VendorRef{VendorID: "vendor-1", VendorNodeCode: "FC-CODE-1"}
	}
	n, err := usecases.NewRegisterNode(h.deps).Execute(t.Context(), cmd)
	must(t, err)
	return n.NodeID
}

func (h *harness) activate(t *testing.T, id string) {
	t.Helper()
	_, err := usecases.NewActivateNode(h.deps).Execute(t.Context(), node.ID(id))
	must(t, err)
}

func (h *harness) seed(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	north, err := usecases.NewDefineRegion(h.deps).Execute(ctx, usecases.DefineRegionCommand{Code: "NORTH", Name: "North"})
	must(t, err)
	south, err := usecases.NewDefineRegion(h.deps).Execute(ctx, usecases.DefineRegionCommand{Code: "SOUTH", Name: "South"})
	must(t, err)

	fc := h.register(t, node.FulfillmentCenter, node.OperatorVendor, north.RegionID, "01310")
	_, err = usecases.NewDeclareOperationalProfile(h.deps).Execute(ctx, usecases.DeclareOperationalProfileCommand{
		NodeID: node.ID(fc), Declaration: repocontract.Declaration(1),
	})
	must(t, err)
	h.activate(t, fc)
	sc := h.register(t, node.SortationCenter, node.OperatorNetwork, north.RegionID, "01420")
	h.activate(t, sc)
	ds := h.register(t, node.DeliveryStation, node.OperatorNetwork, north.RegionID, "01550")
	h.activate(t, ds)
	proposed := h.register(t, node.DeliveryStation, node.OperatorNetwork, south.RegionID, "80010")

	_, err = usecases.NewAssignServiceArea(h.deps).Execute(ctx, usecases.AssignServiceAreaCommand{
		PostalPrefix: servicearea.PostalPrefix("013"), DeliveryStationNodeID: node.ID(ds), AssignmentVersion: 1,
	})
	must(t, err)
	h.w = world{fcID: fc, scID: sc, dsID: ds, proposedID: proposed, northID: north.RegionID, southID: south.RegionID}
}

// readDeps is the A2A Deps over the harness repositories.
func (h *harness) readDeps() inbounda2a.Deps {
	return inbounda2a.Deps{
		GetNode:            usecases.NewGetNode(h.deps.Nodes),
		ListNodes:          usecases.NewListNodes(h.deps.Nodes),
		ResolveServiceArea: usecases.NewResolveServiceArea(h.deps.ServiceAreas),
	}
}

// ---- the wire: JSON-RPC over HTTP, driven by the SDK's own client ----------

// wireEnvelope is a TEST-LOCAL copy of the answer contract. If the adapter's
// envelope drifts, this suite fails instead of silently following it.
type wireNode struct {
	NodeID          string `json:"nodeId"`
	NodeType        string `json:"nodeType"`
	Subtype         string `json:"subtype"`
	Operator        string `json:"operator"`
	RegionID        string `json:"regionId"`
	Timezone        string `json:"timezone"`
	PostalCode      string `json:"postalCode"`
	CountryCode     string `json:"countryCode"`
	Status          string `json:"status"`
	ProfileRevision int    `json:"profileRevision"`
	Capabilities    struct {
		Outbound bool `json:"outbound"`
		LastMile bool `json:"lastMile"`
	} `json:"capabilities"`
	Profile *struct {
		ProfileRevision     int                        `json:"profileRevision"`
		HandlingTimeMinutes int                        `json:"handlingTimeMinutes"`
		DeclaredBy          string                     `json:"declaredBy"`
		OperatingHoursByDay map[string]json.RawMessage `json:"operatingHoursByDay"`
	} `json:"operationalProfile"`
}

type wireEnvelope struct {
	Skill string `json:"skill"`
	Data  struct {
		ServingStation *struct {
			PostalPrefix          string   `json:"postalPrefix"`
			DeliveryStationNodeID string   `json:"deliveryStationNodeId"`
			RegionID              string   `json:"regionId"`
			AssignmentVersion     int      `json:"assignmentVersion"`
			Station               wireNode `json:"station"`
		} `json:"servingStation"`
		Nodes *struct {
			Count int        `json:"count"`
			Nodes []wireNode `json:"nodes"`
		} `json:"nodeList"`
	} `json:"data"`
	Problem *struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	} `json:"problem"`
}

type wire struct{ client *a2aclient.Client }

func newWire(t *testing.T, deps inbounda2a.Deps) *wire {
	t.Helper()
	srv := httptest.NewServer(inbounda2a.Handler(inbounda2a.NewExecutor(deps)))
	t.Cleanup(srv.Close)
	card := inbounda2a.Card(inbounda2a.Config{AgentURL: srv.URL + "/", Version: "test"})
	client, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("NewFromCard: %v", err)
	}
	return &wire{client: client}
}

// reply is the decoded answer: the envelope, its raw JSON, the one-line text
// summary and the whole message.
type reply struct {
	env  wireEnvelope
	raw  string
	text string
	msg  *a2a.Message
}

// send sends one message and returns the decoded reply.
func (w *wire) send(t *testing.T, msg *a2a.Message) reply {
	t.Helper()
	resp, err := w.client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatalf("SendMessage: %v (a domain or request problem must be ANSWERED in-band, never a JSON-RPC error)", err)
	}
	m, ok := resp.(*a2a.Message)
	if !ok || len(m.Parts) < 2 {
		t.Fatalf("reply = %T, want a message with a data and a text part", resp)
	}
	data, ok := m.Parts[0].Content.(a2a.Data)
	if !ok {
		t.Fatalf("first reply part is %T, want data", m.Parts[0].Content)
	}
	raw, _ := json.Marshal(data.Value)
	var env wireEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("reply is not the skill envelope: %v (%s)", err, raw)
	}
	text, _ := m.Parts[1].Content.(a2a.Text)
	return reply{env: env, raw: string(raw), text: string(text), msg: m}
}

func (w *wire) call(t *testing.T, part *a2a.Part) reply {
	t.Helper()
	return w.send(t, a2a.NewMessage(a2a.MessageRoleUser, part))
}

func (w *wire) skill(t *testing.T, skill string, args map[string]any) reply {
	t.Helper()
	return w.call(t, a2a.NewDataPart(map[string]any{"skill": skill, "arguments": args}))
}

func wantProblem(t *testing.T, r reply, status int, slug string) {
	t.Helper()
	if r.env.Problem == nil {
		t.Fatalf("expected a %d %s problem, got data: %s", status, slug, r.raw)
	}
	if r.env.Problem.Status != status || r.env.Problem.Type != slug {
		t.Fatalf("problem = %+v, want %d %s", r.env.Problem, status, slug)
	}
	if r.env.Data.ServingStation != nil || r.env.Data.Nodes != nil {
		t.Fatalf("a problem answer must carry no data: %s", r.raw)
	}
	if !strings.Contains(r.text, r.env.Problem.Title) {
		t.Errorf("text summary %q does not carry the problem title %q", r.text, r.env.Problem.Title)
	}
}

func TestWireServingStationResolvesTheAreaAndTheStationNode(t *testing.T) {
	h := newHarness(t)
	w := newWire(t, h.readDeps())

	r := w.skill(t, "serving_station", map[string]any{"postalPrefix": "013"})
	got := r.env.Data.ServingStation
	if r.env.Skill != "serving_station" || got == nil || r.env.Problem != nil {
		t.Fatalf("answer = %s", r.raw)
	}
	if got.PostalPrefix != "013" || got.DeliveryStationNodeID != h.w.dsID || got.RegionID != h.w.northID || got.AssignmentVersion != 1 {
		t.Errorf("service area = %+v", got)
	}
	assertStationNode(t, got.Station, h.w.dsID)
	if !strings.Contains(r.text, "013") || !strings.Contains(r.text, h.w.dsID) {
		t.Errorf("summary %q must name the prefix and the station", r.text)
	}
	if strings.Contains(r.raw, "vendorRef") || strings.Contains(r.raw, `"location"`) {
		t.Errorf("domain/view structs must not leak across the boundary: %s", r.raw)
	}
}

func assertStationNode(t *testing.T, st wireNode, dsID string) {
	t.Helper()
	if st.NodeID != dsID || st.NodeType != "DELIVERY_STATION" || st.Operator != "NETWORK" || st.Status != "ACTIVE" ||
		st.PostalCode != "01550" || st.CountryCode != "BR" || !st.Capabilities.LastMile {
		t.Errorf("station = %+v", st)
	}
}

func TestWireServingStationCarriesTheStationProfile(t *testing.T) {
	h := newHarness(t)
	// Declare a profile on the delivery station so the skill's "when does it
	// operate" half is exercised with real data (revision 2 on top of the
	// profile network-operated nodes start with, if any).
	profile, err := usecases.NewGetNode(h.deps.Nodes).Execute(t.Context(), node.ID(h.w.dsID))
	must(t, err)
	rev := 1
	if profile.OperationalProfile != nil {
		rev = profile.OperationalProfile.ProfileRevision + 1
	}
	_, err = usecases.NewDeclareOperationalProfile(h.deps).Execute(t.Context(), usecases.DeclareOperationalProfileCommand{
		NodeID: node.ID(h.w.dsID), Declaration: repocontract.Declaration(rev),
	})
	must(t, err)

	r := newWire(t, h.readDeps()).skill(t, "serving_station", map[string]any{"postalPrefix": "013"})
	st := r.env.Data.ServingStation.Station
	if st.Profile == nil || st.Profile.ProfileRevision != rev || st.Profile.HandlingTimeMinutes != 30 || len(st.Profile.OperatingHoursByDay) == 0 {
		t.Fatalf("station profile = %+v, want revision %d with hours and handling time", st.Profile, rev)
	}
}

func TestWireFindNodesFilters(t *testing.T) {
	h := newHarness(t)
	w := newWire(t, h.readDeps())

	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"every node", map[string]any{}, []string{h.w.fcID, h.w.scID, h.w.dsID, h.w.proposedID}},
		{"by type", map[string]any{"type": "DELIVERY_STATION"}, []string{h.w.dsID, h.w.proposedID}},
		{"by status", map[string]any{"status": "ACTIVE"}, []string{h.w.fcID, h.w.scID, h.w.dsID}},
		{"by region", map[string]any{"regionId": h.w.southID}, []string{h.w.proposedID}},
		{"filters combine with AND", map[string]any{"type": "DELIVERY_STATION", "status": "ACTIVE", "regionId": h.w.northID}, []string{h.w.dsID}},
		{"no match is an empty list", map[string]any{"type": "AIR_HUB"}, nil},
		{"unknown region matches nothing", map[string]any{"regionId": "region-nope"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := w.skill(t, "find_nodes", tt.args)
			list := r.env.Data.Nodes
			if r.env.Skill != "find_nodes" || list == nil || r.env.Problem != nil {
				t.Fatalf("answer = %s", r.raw)
			}
			var got []string
			for _, n := range list.Nodes {
				got = append(got, n.NodeID)
			}
			if !sameSet(got, tt.want) || list.Count != len(tt.want) {
				t.Errorf("nodes = %v (count %d), want %v", got, list.Count, tt.want)
			}
			if len(tt.want) == 0 && !strings.Contains(r.raw, `"nodes":[]`) {
				t.Errorf("an empty answer must carry an empty array, not null: %s", r.raw)
			}
		})
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestWireFindNodesIsCompact(t *testing.T) {
	h := newHarness(t)
	r := newWire(t, h.readDeps()).skill(t, "find_nodes", map[string]any{"type": "FULFILLMENT_CENTER"})
	fc := r.env.Data.Nodes.Nodes[0]
	if fc.NodeType != "FULFILLMENT_CENTER" || fc.Subtype != "SORTABLE" || fc.Operator != "VENDOR" || fc.ProfileRevision != 1 {
		t.Errorf("node = %+v", fc)
	}
	if fc.Profile != nil || strings.Contains(r.raw, "operationalProfile") {
		t.Errorf("a listing carries the profile revision only, never the full profile: %s", r.raw)
	}
}

// Every rejection is an answer in the message envelope with the status the
// REST adapter would use, never a JSON-RPC error.
func TestWireProblemsAreAnsweredInBand(t *testing.T) {
	h := newHarness(t)
	w := newWire(t, h.readDeps())

	tests := []struct {
		name   string
		skill  string
		args   map[string]any
		status int
		slug   string
	}{
		{"unassigned postal prefix is a 404", "serving_station", map[string]any{"postalPrefix": "999"}, 404, "not-found"},
		{"prefix is matched exactly", "serving_station", map[string]any{"postalPrefix": "01"}, 404, "not-found"},
		{"missing postalPrefix", "serving_station", map[string]any{}, 400, "invalid-argument"},
		{"blank postalPrefix", "serving_station", map[string]any{"postalPrefix": "   "}, 400, "invalid-argument"},
		{"NUL in postalPrefix", "serving_station", map[string]any{"postalPrefix": "01\x003"}, 400, "invalid-argument"},
		{"unknown argument", "serving_station", map[string]any{"postalPrefix": "013", "prefix": "013"}, 400, "invalid-argument"},
		{"wrong argument type", "serving_station", map[string]any{"postalPrefix": 13}, 400, "invalid-argument"},
		{"find_nodes unknown argument", "find_nodes", map[string]any{"regionID": "x", "colour": "red"}, 400, "invalid-argument"},
		{"find_nodes misspelt filter is refused, not ignored", "find_nodes", map[string]any{"statuz": "ACTIVE"}, 400, "invalid-argument"},
		{"NUL in a filter", "find_nodes", map[string]any{"regionId": "a\x00b"}, 400, "invalid-argument"},
		{"unknown node type is a validation failure", "find_nodes", map[string]any{"type": "WAREHOUSE"}, 422, "validation-failed"},
		{"unknown node status is a validation failure", "find_nodes", map[string]any{"status": "BROKEN"}, 422, "validation-failed"},
		{"unknown skill", "get_weather", map[string]any{}, 400, "unknown-skill"},
		{"a write skill does not exist: register_node", "register_node", map[string]any{"type": "AIR_HUB"}, 400, "unknown-skill"},
		{"a write skill does not exist: assign_service_area", "assign_service_area", map[string]any{"postalPrefix": "013"}, 400, "unknown-skill"},
		{"a write skill does not exist: suspend_node", "suspend_node", map[string]any{"nodeId": "x"}, 400, "unknown-skill"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantProblem(t, w.skill(t, tt.skill, tt.args), tt.status, tt.slug)
		})
	}
}

func TestWireMalformedRequestsAreAnsweredInBand(t *testing.T) {
	h := newHarness(t)
	w := newWire(t, h.readDeps())

	tests := []struct {
		name string
		part *a2a.Part
	}{
		{"data part without a skill", a2a.NewDataPart(map[string]any{"arguments": map[string]any{}})},
		{"unknown top-level field", a2a.NewDataPart(map[string]any{"skill": "find_nodes", "arguments": map[string]any{}, "extra": 1})},
		{"text part that is not JSON", a2a.NewTextPart("which station serves 013?")},
		{"text part with trailing data", a2a.NewTextPart(`{"skill":"find_nodes","arguments":{}} {"skill":"find_nodes"}`)},
		{"text part with trailing junk", a2a.NewTextPart(`{"skill":"find_nodes"} junk`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantProblem(t, w.call(t, tt.part), 400, "malformed-skill-request")
		})
	}
}

// A skill request may be a text part holding the same JSON.
func TestWireAcceptsATextPartHoldingTheSkillJSON(t *testing.T) {
	h := newHarness(t)
	r := newWire(t, h.readDeps()).call(t, a2a.NewTextPart(`{"skill":"serving_station","arguments":{"postalPrefix":"013"}}`))
	if r.env.Data.ServingStation == nil || r.env.Data.ServingStation.DeliveryStationNodeID != h.w.dsID {
		t.Fatalf("answer = %s", r.raw)
	}
}

// Absent arguments are the empty object: find_nodes lists everything.
func TestWireAbsentArgumentsAreTheEmptyObject(t *testing.T) {
	h := newHarness(t)
	w := newWire(t, h.readDeps())
	r := w.call(t, a2a.NewDataPart(map[string]any{"skill": "find_nodes"}))
	if r.env.Data.Nodes == nil || r.env.Data.Nodes.Count != 4 {
		t.Fatalf("answer = %s", r.raw)
	}
	wantProblem(t, w.call(t, a2a.NewDataPart(map[string]any{"skill": "serving_station"})), 400, "invalid-argument")
}

// The answer is bound to the caller's context so a delegating saga can group
// its interactions.
func TestWireReplyCarriesTheCallersContext(t *testing.T) {
	h := newHarness(t)
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"skill": "find_nodes", "arguments": map[string]any{}}))
	msg.ContextID = "ctx-123"
	r := newWire(t, h.readDeps()).send(t, msg)
	if r.msg.ContextID != "ctx-123" {
		t.Errorf("reply contextId = %q, want ctx-123", r.msg.ContextID)
	}
}

// Read-only is structural: after every skill and every problem the store
// holds exactly the rows the seeding wrote (no node, profile, lane, area,
// region or OUTBOX row appears, no aggregate version moves).
func TestWireSkillsNeverWrite(t *testing.T) {
	h := newHarness(t)
	before := h.store.Counts()
	w := newWire(t, h.readDeps())
	w.skill(t, "serving_station", map[string]any{"postalPrefix": "013"})
	w.skill(t, "serving_station", map[string]any{"postalPrefix": "999"})
	w.skill(t, "find_nodes", map[string]any{})
	w.skill(t, "find_nodes", map[string]any{"type": "WAREHOUSE"})
	w.skill(t, "register_node", map[string]any{})
	if after := h.store.Counts(); after != before {
		t.Fatalf("store changed through the read-only agent: %+v -> %+v", before, after)
	}
	if before.OutboxEvents == 0 {
		t.Fatal("the seed must have produced outbox rows, or the no-write assertion proves nothing")
	}
}

// failingNodes is a node repository whose reads fail with an infrastructure
// error carrying text that must never reach an agent.
type failingNodes struct{ ports.NodeRepository }

var errInfra = errors.New("dial tcp 10.0.0.7:5432: password authentication failed for user svc_secret")

func (failingNodes) List(context.Context, repository.NodeFilter) ([]*node.Node, error) {
	return nil, errInfra
}

func TestWireInfrastructureFailureIsAnInternalProblemWithoutLeakingText(t *testing.T) {
	h := newHarness(t)
	deps := h.readDeps()
	deps.ListNodes = usecases.NewListNodes(failingNodes{h.deps.Nodes})
	r := newWire(t, deps).skill(t, "find_nodes", map[string]any{})
	wantProblem(t, r, 500, "internal-error")
	if strings.Contains(r.raw, "svc_secret") || strings.Contains(r.text, "svc_secret") || strings.Contains(r.raw, "10.0.0.7") {
		t.Fatalf("an internal error text leaked to the agent: %s / %s", r.raw, r.text)
	}
}

// Each skill runs inside an a2a.skill span carrying its id; a request that is
// not a skill request has no skill to name and so no span.
func TestSkillSpanCarriesTheSkillAttribute(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	h := newHarness(t)
	w := newWire(t, h.readDeps())
	w.skill(t, "find_nodes", map[string]any{})
	w.skill(t, "serving_station", map[string]any{"postalPrefix": "999"})

	got := map[string]bool{}
	for _, s := range recorder.Ended() {
		for _, a := range s.Attributes() {
			if a.Key == "a2a.skill" && s.Name() == "a2a.skill "+a.Value.AsString() {
				got[a.Value.AsString()] = true
			}
		}
	}
	if !got["find_nodes"] || !got["serving_station"] {
		t.Fatalf("skill spans = %v, want find_nodes and serving_station", got)
	}
}
