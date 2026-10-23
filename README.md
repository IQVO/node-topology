# node-topology

Retail network, bounded context **Node Topology** (Supporting, logistics tier): the authoritative registry of the network's nodes, their versioned
operational profiles, lanes, regions and postal service areas.

Status: under construction. The design is proposed in
[retail-network-docs](https://iqvo.github.io/retail-network-docs/contexts/node-topology); `model/model.yaml` there is the single source of truth.
This repository is a service of the retail network, not of the warehouse-systems fleet; the fleet reaches the network only through the
`vendor-connectivity` Vendor API.

Runtime: Go, hexagonal, real PostgreSQL and Kafka only (`DATABASE_URL`, `KAFKA_BROKERS` are mandatory), CloudEvents 1.0 on every message
(`com.retailnetwork.logistics.node-topology.<entity>.<Event>`), transactional outbox.

See `docs/adr/` for decisions and `docs/before-first-release.md` for the packaging checklist.
