package kafka

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/claudioed/node-topology/internal/adapters/kafka/cloudevents"
	"github.com/claudioed/node-topology/internal/application/outbox"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// TopicEvents is this service's integration-event topic. Every event the
// service publishes is an integration event, so it is the only topic written
// (apis/asyncapi.yaml); the analytics topic stays unused.
const TopicEvents = "retailnetwork.node-topology.events"

// schemaVersion is the dataschema version of every payload
// (urn:retailnetwork:node-topology:events:<Event>:v1).
const schemaVersion = 1

// Encoder implements outbox.Encoder: each domain event becomes one
// outbox.Message holding the CloudEvents bytes, the Kafka key (the aggregate
// id) and the content-type header. The CloudEvents `id` is minted HERE, once,
// and persisted with the outbox row, so a relay retry republishes the same
// id. Envelopes are built only through internal/adapters/kafka/cloudevents.
type Encoder struct {
	// NewID mints a CloudEvents id; uuid.NewString when nil.
	NewID func() string
}

// NewEncoder returns an Encoder minting random UUID v4 ids.
func NewEncoder() *Encoder { return &Encoder{NewID: uuid.NewString} }

var _ outbox.Encoder = (*Encoder)(nil)

// Encode encodes one event. An event type the service does not publish is a
// programming error and fails the call.
func (e *Encoder) Encode(ev shared.Event) (outbox.Message, error) {
	data, err := payloadFor(ev)
	if err != nil {
		return outbox.Message{}, err
	}
	newID := e.NewID
	if newID == nil {
		newID = uuid.NewString
	}
	id := newID()
	value, err := cloudevents.New(cloudevents.Spec{
		ID:        id,
		Entity:    ev.Entity(),
		EventName: ev.EventName(),
		Subject:   ev.SubjectID(),
		Time:      ev.OccurredAt(),
		Stream:    cloudevents.StreamEvents,
		Version:   schemaVersion,
		Data:      data,
	})
	if err != nil {
		return outbox.Message{}, fmt.Errorf("encode %s: %w", ev.EventName(), err)
	}
	ct := cloudevents.ContentTypeHeader()
	return outbox.Message{
		EventID:    id,
		Topic:      TopicEvents,
		EventType:  cloudevents.Type(ev.Entity(), ev.EventName()),
		Subject:    ev.SubjectID(),
		Key:        []byte(ev.SubjectID()),
		DataSchema: cloudevents.DataSchema(cloudevents.StreamEvents, ev.EventName(), schemaVersion),
		Value:      value,
		Headers:    []outbox.Header{{Key: ct.Key, Value: string(ct.Value)}},
	}, nil
}

// payloadFor maps a domain event to its wire `data` (apis/asyncapi.yaml).
func payloadFor(ev shared.Event) (any, error) {
	switch e := ev.(type) {
	case node.NodeRegistered:
		return registeredData(e), nil
	case node.NodeOperationalProfileDeclared:
		return profileDeclaredData(e), nil
	case node.NodeActivated:
		return activatedPayload{NodeID: string(e.NodeID), ActivatedAt: instant(e.ActivatedAt)}, nil
	case node.NodeSuspended:
		return suspendedPayload{NodeID: string(e.NodeID), Reason: e.Reason, SuspendedAt: instant(e.SuspendedAt)}, nil
	case node.NodeRetired:
		return retiredPayload{NodeID: string(e.NodeID), RetiredAt: instant(e.RetiredAt)}, nil
	case lane.LaneOpened:
		return laneOpenedPayload{
			LaneID: string(e.LaneID), OriginNodeID: string(e.OriginNodeID), DestinationNodeID: string(e.DestinationNodeID),
			Mode: string(e.Mode), NominalTransitMinutes: e.NominalTransitMinutes,
		}, nil
	case lane.LaneClosed:
		return laneClosedPayload{LaneID: string(e.LaneID), ClosedAt: instant(e.ClosedAt)}, nil
	case servicearea.ServiceAreaAssigned:
		return serviceAreaPayload{
			PostalPrefix: string(e.PostalPrefix), DeliveryStationNodeID: string(e.DeliveryStationNodeID),
			RegionID: string(e.RegionID), AssignmentVersion: e.AssignmentVersion,
		}, nil
	case region.RegionDefined:
		return regionPayload{RegionID: string(e.RegionID), Code: e.Code, Name: e.Name}, nil
	default:
		return nil, fmt.Errorf("kafka encoder: %T is not a published event", ev)
	}
}

// instant renders a time as RFC 3339 UTC, the wire convention for instants.
func instant(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
