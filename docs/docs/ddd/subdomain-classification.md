---
id: subdomain-classification
title: Subdomain Classification
sidebar_label: Subdomain Classification
description: "Why Node Topology is a Supporting subdomain of the retail network, and how its neighbours are classified."
---

# Subdomain Classification

Domain-Driven Design splits a domain into **Core**, **Supporting** and **Generic** subdomains by how much competitive advantage each gives, not by size or difficulty. The network's own table is on the warehouse-docs site: [Subdomain classification](https://iqvo.github.io/warehouse-docs/network/strategic-design/subdomain-classification). This page records the verdict for this context and the reasoning behind it.

## Verdict

| Bounded context | Classification | CloudEvents tier | Business model | Evolution |
| --- | --- | --- | --- | --- |
| `node-topology` | <span class="badge-supporting">Supporting</span> | `logistics` | Enabler | Custom-built |

The problem-space subdomain it implements is **Network Topology**: knowing the nodes, lanes, regions and service areas, and what each node declared. The network's verdict for that subdomain is Supporting.

## Reasoning

- **Not Core.** Every decision the network makes depends on the registry, but nobody chooses a network for how it stores its node list. The competitive decisions are made elsewhere: `delivery-promise` (what arrival date to promise) and `order-sourcing` (which node and path keep the promise at the lowest cost). This context only holds declared facts; it does not schedule, promise, assign or measure.
- **Not Generic.** The parcel-node rule (sortation centres, delivery stations and air hubs are always network-operated, vendors register only the other three types), the allowed lane type pairs and the versioned operational profile with cutoffs and a throughput cap are specific to this network. They cannot be bought as a product without conforming to someone else's model of the network.
- **Why it is built with care anyway.** It sits at the root of the dependency graph: the Core contexts build local read models from its nine events and never call it on their hot path, so a wrong or late fact here propagates to every promise and route. Its invariants are therefore the strictest part of the service (state machines, immutable profile revisions, strictly increasing versions) even though the classification is Supporting.

Evolution is custom-built, drifting towards product once the profile contract settles (the network's Core Domain Chart places `node-topology` at 0.44, 0.42). Source: warehouse-docs `network/docs/strategic-design/subdomain-classification.md` and `core-domain-chart.md` on `origin/main`; the invariants named above are in `internal/domain/node`, `lane` and `servicearea`.

## Neighbours

Copied from the network's classification table:

| Context | Classification | Tier | Relationship to `node-topology` |
| --- | --- | --- | --- |
| `delivery-promise` | <span class="badge-core">Core</span> | `commerce` | Downstream: subscribes to all nine events |
| `order-sourcing` | <span class="badge-core">Core</span> | `commerce` | Downstream: subscribes to all nine events |
| `offer-availability` | <span class="badge-supporting">Supporting</span> | `commerce` | Downstream: node status events |
| `vendor-connectivity` | <span class="badge-supporting">Supporting</span> | `partner` | Downstream (events) and Customer of the two write routes |
| `linehaul-transportation` | <span class="badge-supporting">Supporting</span> | `logistics` | Downstream: node and lane events |
| `last-mile-delivery` | <span class="badge-supporting">Supporting</span> | `logistics` | Downstream: `NodeRegistered`, `ServiceAreaAssigned` |
| `node-performance` | <span class="badge-supporting">Supporting</span> | `partner` | Downstream: `NodeRegistered`, `NodeOperationalProfileDeclared` |
| `seller-inbound` | <span class="badge-supporting">Supporting</span> | `partner` | Downstream: node events including the inbound limits |
| `customer-ordering` | <span class="badge-generic">Generic</span> | `commerce` | none (Separate Ways) |
| `shipping-and-tracking` | <span class="badge-generic">Generic</span> | `logistics` | none (Separate Ways) |

The network has two Core, seven Supporting and two Generic contexts in that table. The two Core contexts are consumers of this one: the strongest dependency in the network points at a Supporting context, which is why its contract (nine events, schema URNs, `v1` types that change only by a new `.v2` type) is treated as a published language.
