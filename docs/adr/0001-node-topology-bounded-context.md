# 1. Node Topology is a bounded context of the retail network

Status: Accepted (bootstrap). Design source: retail-network-docs `model/model.yaml`, context `node-topology`, and its ADRs RN-0001, RN-0004 and RN-0007.

## Context

The retail network is modelled as ten bounded contexts, each its own service and database. Node Topology owns the physical shape of the network and
publishes it to every other context. It consumes no Kafka topic.

## Decision

- This repo owns aggregates Node, Lane, ServiceArea and Region; the invariants and error names are those of the model.
- Parcel nodes (SORTATION_CENTER, DELIVERY_STATION, AIR_HUB) are network-operated only (ErrOperatorNotAllowedForNodeType).
- Relationships: Open Host Service to every reader of nodes, lanes and service areas; Customer/Supplier with `vendor-connectivity`, the only caller that registers vendor nodes.
- Events use the network CloudEvents namespace `com.retailnetwork.logistics.node-topology.<entity>.<Event>` on `retailnetwork.node-topology.events`. No `warehouse.*` topic is consumed or produced.
- Excluded: purchase orders and vendor protocol (vendor-connectivity), departures (linehaul-transportation), scorecards (node-performance).
- No live REST lookup into sibling contexts: this context calls none.
- No authentication on REST, consistent with the fleet; no PII exists here.

## Consequences

The service has no consumers and one outbox relay. Everything else is a reader of its events or of its OHS queries.
