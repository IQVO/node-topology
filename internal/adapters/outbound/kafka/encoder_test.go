package kafka

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/adapters/kafka/cloudevents"
	"github.com/claudioed/node-topology/internal/application/outbox"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

const goldenID = "1b0c9a4e-2f7d-4a63-9d1e-5a7c3e2b9f10"

func fixedID() string { return goldenID }

var (
	at     = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	later  = time.Date(2026, 10, 8, 12, 30, 15, 123456000, time.UTC)
	header = shared.Header{Version: 3, At: at}
)

func envelope(entity, name, subject, timeStr, data string) string {
	return `{"specversion":"1.0","id":"` + goldenID + `","source":"/retailnetwork/node-topology",` +
		`"type":"com.retailnetwork.logistics.node-topology.` + entity + `.` + name + `","subject":"` + subject + `",` +
		`"datacontenttype":"application/json","dataschema":"urn:retailnetwork:node-topology:events:` + name + `:v1",` +
		`"time":"` + timeStr + `","data":` + data + `}`
}

type goldenCase struct {
	name    string
	event   shared.Event
	entity  string
	subject string
	want    string
}

func goldenCases() []goldenCase {
	return []goldenCase{
		registeredVendorCase(), registeredNetworkCase(), profileSortCase(), profileMinimalCase(), profileInboundCase(), profilePartialInboundCase(),
		{
			"NodeActivated", node.NodeActivated{Header: header, NodeID: "node-1", ActivatedAt: later},
			"node", "node-1", `{"nodeId":"node-1","activatedAt":"2026-10-08T12:30:15.123456Z"}`,
		},
		{
			"NodeSuspended", node.NodeSuspended{Header: header, NodeID: "node-1", Reason: "dock closed", SuspendedAt: at},
			"node", "node-1", `{"nodeId":"node-1","reason":"dock closed","suspendedAt":"2026-10-08T12:00:00Z"}`,
		},
		{
			"NodeRetired", node.NodeRetired{Header: header, NodeID: "node-1", RetiredAt: at},
			"node", "node-1", `{"nodeId":"node-1","retiredAt":"2026-10-08T12:00:00Z"}`,
		},
		{
			"LaneOpened", lane.LaneOpened{Header: header, LaneID: "lane-1", OriginNodeID: "node-1", DestinationNodeID: "node-2", Mode: lane.ModeAir, NominalTransitMinutes: 95},
			"lane", "lane-1", `{"laneId":"lane-1","originNodeId":"node-1","destinationNodeId":"node-2","mode":"AIR","nominalTransitMinutes":95}`,
		},
		{
			"LaneClosed", lane.LaneClosed{Header: header, LaneID: "lane-1", ClosedAt: at},
			"lane", "lane-1", `{"laneId":"lane-1","closedAt":"2026-10-08T12:00:00Z"}`,
		},
		{
			"ServiceAreaAssigned", servicearea.ServiceAreaAssigned{Header: header, PostalPrefix: "01310", DeliveryStationNodeID: "node-9", RegionID: "region-1", AssignmentVersion: 2},
			"servicearea", "01310", `{"postalPrefix":"01310","deliveryStationNodeId":"node-9","regionId":"region-1","assignmentVersion":2}`,
		},
		{
			"RegionDefined", region.RegionDefined{Header: header, RegionID: "region-1", Code: "SE", Name: "Southeast"},
			"region", "region-1", `{"regionId":"region-1","code":"SE","name":"Southeast"}`,
		},
	}
}

func registeredVendorCase() goldenCase {
	return goldenCase{
		"NodeRegistered vendor FC",
		node.NodeRegistered{
			Header: header, NodeID: "node-1", NodeType: node.FulfillmentCenter, Subtype: node.SubtypeXL, Operator: node.OperatorVendor,
			VendorID: "vendor-7", VendorNodeCode: "FC-A", RegionID: "region-1", Timezone: "America/Sao_Paulo", PostalCode: "01310-100", CountryCode: "BR",
		},
		"node", "node-1",
		`{"nodeId":"node-1","nodeType":"FULFILLMENT_CENTER","subtype":"XL","operator":"VENDOR","vendorId":"vendor-7","vendorNodeCode":"FC-A",` +
			`"regionId":"region-1","timezone":"America/Sao_Paulo","postalCode":"01310-100","countryCode":"BR"}`,
	}
}

func registeredNetworkCase() goldenCase {
	return goldenCase{
		"NodeRegistered network sort centre (nullable fields are null)",
		node.NodeRegistered{
			Header: header, NodeID: "node-2", NodeType: node.SortationCenter, Operator: node.OperatorNetwork,
			RegionID: "region-1", Timezone: "America/Sao_Paulo", PostalCode: "06000-000", CountryCode: "BR",
		},
		"node", "node-2",
		`{"nodeId":"node-2","nodeType":"SORTATION_CENTER","subtype":null,"operator":"NETWORK","vendorId":null,"vendorNodeCode":null,` +
			`"regionId":"region-1","timezone":"America/Sao_Paulo","postalCode":"06000-000","countryCode":"BR"}`,
	}
}

func profileSortCase() goldenCase {
	dwell := 45
	return goldenCase{
		"NodeOperationalProfileDeclared sort centre",
		node.NodeOperationalProfileDeclared{
			Header: header, NodeID: "node-2", ProfileRevision: 2, Timezone: "America/Sao_Paulo",
			OperatingHoursByDay: node.OperatingHours{
				node.Sunday: {Start: "08:00", End: "12:00"}, node.Monday: {Start: "06:00", End: "22:00"}, node.Friday: {Start: "06:00", End: "20:00"},
			},
			Holidays: []string{"2026-12-25"}, HandlingTimeMinutes: 90,
			Cutoffs: []node.CutoffSlot{
				{LocalTime: "16:00", Days: []node.Weekday{node.Monday, node.Friday}, MaxOrders: 500},
				{LocalTime: "18:30"},
			},
			ThroughputCap:    node.ThroughputCap{Value: 1200, TimeUnit: node.TimeUnitHour, Unit: node.UnitOrder},
			SortDwellMinutes: &dwell, DeclaredBy: node.DeclaredByNetworkOps,
		},
		"node", "node-2",
		`{"nodeId":"node-2","profileRevision":2,"timezone":"America/Sao_Paulo",` +
			`"operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"},"friday":{"start":"06:00","end":"20:00"},"sunday":{"start":"08:00","end":"12:00"}},` +
			`"holidays":["2026-12-25"],"handlingTimeMinutes":90,` +
			`"cutoffs":[{"localTime":"16:00","days":["monday","friday"],"maxOrders":500},{"localTime":"18:30"}],` +
			`"throughputCap":{"value":1200,"timeUnit":"HOUR","unit":"ORDER"},"sortDwellMinutes":45,"declaredBy":"NETWORK_OPS"}`,
	}
}

func profileMinimalCase() goldenCase {
	return goldenCase{
		"NodeOperationalProfileDeclared vendor FC without holidays or dwell",
		node.NodeOperationalProfileDeclared{
			Header: header, NodeID: "node-1", ProfileRevision: 1, Timezone: "America/Sao_Paulo",
			OperatingHoursByDay: node.OperatingHours{node.Tuesday: {Start: "09:00", End: "17:00"}, node.Wednesday: {Start: "09:00", End: "17:00"},
				node.Thursday: {Start: "09:00", End: "17:00"}, node.Saturday: {Start: "09:00", End: "13:00"}},
			HandlingTimeMinutes: 0, Cutoffs: []node.CutoffSlot{{LocalTime: "15:00"}},
			ThroughputCap: node.ThroughputCap{Value: 10, TimeUnit: node.TimeUnitDay, Unit: node.UnitOrder}, DeclaredBy: node.DeclaredByVendor,
		},
		"node", "node-1",
		`{"nodeId":"node-1","profileRevision":1,"timezone":"America/Sao_Paulo",` +
			`"operatingHoursByDay":{"tuesday":{"start":"09:00","end":"17:00"},"wednesday":{"start":"09:00","end":"17:00"},` +
			`"thursday":{"start":"09:00","end":"17:00"},"saturday":{"start":"09:00","end":"13:00"}},` +
			`"holidays":[],"handlingTimeMinutes":0,"cutoffs":[{"localTime":"15:00"}],` +
			`"throughputCap":{"value":10,"timeUnit":"DAY","unit":"ORDER"},"declaredBy":"VENDOR"}`,
	}
}

func profileInboundCase() goldenCase {
	weight, yes, no := 25000, true, false
	return goldenCase{
		"NodeOperationalProfileDeclared with every inbound limit",
		node.NodeOperationalProfileDeclared{
			Header: header, NodeID: "node-3", ProfileRevision: 3, Timezone: "America/Sao_Paulo",
			OperatingHoursByDay: node.OperatingHours{node.Monday: {Start: "06:00", End: "22:00"}},
			HandlingTimeMinutes: 60, Cutoffs: []node.CutoffSlot{{LocalTime: "12:00"}},
			ThroughputCap: node.ThroughputCap{Value: 50, TimeUnit: node.TimeUnitHour, Unit: node.UnitOrder},
			Inbound: &node.InboundLimits{
				MaxCartonWeightG: &weight, MaxCartonDimsMm: &node.CartonDims{L: 600, W: 400, H: 300},
				AcceptsPallets: &yes, RequiresSscc: &no,
			},
			DeclaredBy: node.DeclaredByVendor,
		},
		"node", "node-3",
		`{"nodeId":"node-3","profileRevision":3,"timezone":"America/Sao_Paulo",` +
			`"operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"}},` +
			`"holidays":[],"handlingTimeMinutes":60,"cutoffs":[{"localTime":"12:00"}],` +
			`"throughputCap":{"value":50,"timeUnit":"HOUR","unit":"ORDER"},` +
			`"inbound":{"maxCartonWeightG":25000,"maxCartonDimsMm":{"l":600,"w":400,"h":300},"acceptsPallets":true,"requiresSscc":false},` +
			`"declaredBy":"VENDOR"}`,
	}
}

func profilePartialInboundCase() goldenCase {
	no := false
	return goldenCase{
		"NodeOperationalProfileDeclared with a partial inbound declaration",
		node.NodeOperationalProfileDeclared{
			Header: header, NodeID: "node-4", ProfileRevision: 1, Timezone: "America/Sao_Paulo",
			OperatingHoursByDay: node.OperatingHours{node.Monday: {Start: "06:00", End: "22:00"}},
			HandlingTimeMinutes: 60, Cutoffs: []node.CutoffSlot{{LocalTime: "12:00"}},
			ThroughputCap: node.ThroughputCap{Value: 50, TimeUnit: node.TimeUnitHour, Unit: node.UnitOrder},
			Inbound:       &node.InboundLimits{AcceptsPallets: &no},
			DeclaredBy:    node.DeclaredByNetworkOps,
		},
		"node", "node-4",
		`{"nodeId":"node-4","profileRevision":1,"timezone":"America/Sao_Paulo",` +
			`"operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"}},` +
			`"holidays":[],"handlingTimeMinutes":60,"cutoffs":[{"localTime":"12:00"}],` +
			`"throughputCap":{"value":50,"timeUnit":"HOUR","unit":"ORDER"},` +
			`"inbound":{"acceptsPallets":false},"declaredBy":"NETWORK_OPS"}`,
	}
}

// TestEncoderGoldenWireFormat pins the exact bytes of every published event:
// all CloudEvents attributes plus the payload of apis/asyncapi.yaml, and the
// outbox row around them (topic, id, full type, subject, key, dataschema,
// content-type header).
func TestEncoderGoldenWireFormat(t *testing.T) {
	enc := &Encoder{NewID: fixedID}
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := enc.Encode(tc.event)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			timeStr := tc.event.OccurredAt().UTC().Format(time.RFC3339Nano)
			if want := envelope(tc.entity, tc.event.EventName(), tc.subject, timeStr, tc.want); string(msg.Value) != want {
				t.Fatalf("value\n got: %s\nwant: %s", msg.Value, want)
			}
			assertRow(t, msg, tc)
		})
	}
}

func assertRow(t *testing.T, msg outbox.Message, tc goldenCase) {
	t.Helper()
	wantType := "com.retailnetwork.logistics.node-topology." + tc.entity + "." + tc.event.EventName()
	if msg.EventID != goldenID || msg.Topic != "retailnetwork.node-topology.events" || msg.EventType != wantType {
		t.Errorf("row = id %q topic %q type %q, want %q / retailnetwork.node-topology.events / %q", msg.EventID, msg.Topic, msg.EventType, goldenID, wantType)
	}
	if msg.Subject != tc.subject || string(msg.Key) != tc.subject {
		t.Errorf("subject %q key %q, want both %q", msg.Subject, msg.Key, tc.subject)
	}
	if want := "urn:retailnetwork:node-topology:events:" + tc.event.EventName() + ":v1"; msg.DataSchema != want {
		t.Errorf("dataschema = %q, want %q", msg.DataSchema, want)
	}
	if len(msg.Headers) != 1 || msg.Headers[0].Key != "content-type" || msg.Headers[0].Value != cloudevents.MediaType {
		t.Errorf("headers = %+v, want the CloudEvents content-type only", msg.Headers)
	}
}

// TestEncoderCoversEveryPublishedEvent keeps the golden table in step with
// the catalogue: nine events, nine distinct names.
func TestEncoderCoversEveryPublishedEvent(t *testing.T) {
	names := map[string]bool{}
	for _, tc := range goldenCases() {
		names[tc.event.EventName()] = true
	}
	if len(names) != 9 {
		t.Fatalf("golden table covers %d distinct events, want 9: %v", len(names), names)
	}
}

// TestEncodedMessagesDecodeAsCloudEvents proves the bytes are a valid
// CloudEvents 1.0 event whose data is a JSON object.
func TestEncodedMessagesDecodeAsCloudEvents(t *testing.T) {
	enc := NewEncoder()
	for _, tc := range goldenCases() {
		msg, err := enc.Encode(tc.event)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		e, err := cloudevents.Decode(msg.Value)
		if err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		var data map[string]any
		if err := json.Unmarshal(e.Data(), &data); err != nil {
			t.Fatalf("%s: data is not a JSON object: %v", tc.name, err)
		}
		if e.ID() != msg.EventID || e.Type() != msg.EventType || e.Subject() != msg.Subject {
			t.Errorf("%s: envelope id/type/subject differ from the outbox row", tc.name)
		}
	}
}

// TestEncoderMintsADistinctIDPerCall: the id is minted once per occurrence.
func TestEncoderMintsADistinctIDPerCall(t *testing.T) {
	enc := NewEncoder()
	ev := node.NodeActivated{Header: header, NodeID: "node-1", ActivatedAt: at}
	a, errA := enc.Encode(ev)
	b, errB := enc.Encode(ev)
	if errA != nil || errB != nil {
		t.Fatalf("encode: %v %v", errA, errB)
	}
	if a.EventID == "" || a.EventID == b.EventID {
		t.Fatalf("ids %q and %q must be non-empty and distinct", a.EventID, b.EventID)
	}
	var zero Encoder
	if _, err := zero.Encode(ev); err != nil {
		t.Fatalf("zero-value encoder must mint uuids: %v", err)
	}
}

type notPublished struct{ shared.Header }

func (notPublished) EventName() string { return "Surprise" }
func (notPublished) Entity() string    { return "node" }
func (notPublished) SubjectID() string { return "node-1" }

func TestEncoderRejectsAnEventItDoesNotPublish(t *testing.T) {
	_, err := NewEncoder().Encode(notPublished{header})
	if err == nil || !strings.Contains(err.Error(), "not a published event") {
		t.Fatalf("err = %v, want a not-a-published-event error", err)
	}
}

func TestEncoderRejectsAnEmptySubject(t *testing.T) {
	_, err := NewEncoder().Encode(node.NodeActivated{Header: header, ActivatedAt: at})
	if err == nil || !strings.Contains(err.Error(), "empty subject") {
		t.Fatalf("err = %v, want an empty-subject error", err)
	}
}
