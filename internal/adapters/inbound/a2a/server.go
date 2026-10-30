// Package a2a is the inbound Agent2Agent adapter: it exposes this bounded
// context to the fleet's agent ecosystem as a third driving adapter over the
// same application-layer read use cases the REST and MCP adapters use. It is
// built on the official A2A Go SDK (a2aproject/a2a-go) and speaks JSON-RPC 2.0
// over HTTP, with the Agent Card served at the well-known path.
//
// Per docs/adr/0011-a2a-inbound-adapter.md, A2A is the DELEGATION channel
// between agents: a client sends a goal-shaped skill request as a structured
// message part and this agent answers from its own state through its own use
// cases. It is not a data-sync channel: facts between contexts still travel
// exclusively as CloudEvents on Kafka (ADR 0004).
//
// The surface is READ-ONLY, exactly like the MCP surface (ADR 0008): Deps holds
// read use cases and nothing that can write, so a delegated goal can never
// append to the outbox. Registering nodes, profile revisions, lifecycle
// transitions, lanes, service areas and regions stay REST operations.
//
// Like the MCP adapter, this package depends inward on the application layer
// and the domain only, never on an outbound adapter. The composition root
// (cmd/a2a) wires concrete repositories into the use cases. There is no auth
// of any kind (fleet-wide revert 2026-09-11).
package a2a

import (
	"context"
	"iter"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/claudioed/node-topology/internal/application/usecases"
)

// Config carries the deployment-specific identity of this agent: the absolute
// URL clients use to reach its JSON-RPC endpoint (advertised on the Agent
// Card) and the card version. It is supplied by the composition root from
// A2A_AGENT_URL; the card MUST advertise a URL the agent clients can actually
// dial (in-cluster: http://<release>-a2a.<ns>.svc.cluster.local:8093/).
type Config struct {
	AgentURL string
	Version  string
}

// Deps is everything the A2A skills need, injected by the composition root.
// It carries the SAME read use cases the REST and MCP adapters expose (GET
// /nodes, GET /nodes/{nodeId}, GET /service-areas/{postalPrefix}); the adapter
// never constructs an outbound adapter itself and holds no write use case at
// all (docs/adr/0011, pinned by the architecture fitness tests).
type Deps struct {
	GetNode            *usecases.GetNode
	ListNodes          *usecases.ListNodes
	ResolveServiceArea *usecases.ResolveServiceArea
}

// Card builds this agent's Agent Card. The surface is two curated, goal-shaped
// READ skills, not one skill per endpoint (the same curation discipline ADR
// 0008 applied to MCP tools): serving_station answers "which delivery station
// serves this postal prefix and when does it operate" by composing the service
// area and the station node; find_nodes answers "which nodes match this type,
// region and status". The card declares no security schemes on purpose: this
// surface is unauthenticated by the same fleet-wide decision as REST and MCP.
func Card(cfg Config) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "node-topology",
		Description: "Node Topology agent of the retail network (read-only): answers which delivery station serves a postal prefix, with that station's region and current operational profile (operating hours, cutoffs, handling time, throughput cap), and finds the network's nodes by type, region and status. It reads the authoritative registry of nodes, regions and postal service areas; it never changes it (registering nodes, profile revisions, lifecycle transitions, lanes, service areas and regions are REST operations of node-topology).",
		Version:     cfg.Version,
		Provider: &a2a.AgentProvider{
			Org: "IQVO",
			URL: "https://github.com/IQVO/node-topology",
		},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(cfg.AgentURL, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"application/json", "text/plain"},
		DefaultOutputModes: []string{"application/json", "text/plain"},
		Capabilities:       a2a.AgentCapabilities{Streaming: false},
		Skills: []a2a.AgentSkill{
			{
				ID:          skillServingStation,
				Name:        "Delivery station serving a postal prefix",
				Description: "Composed read: resolves the delivery station that serves a postal prefix (matched exactly) and returns the assignment (deliveryStationNodeId, regionId, assignmentVersion) together with that station's node: type, operator, region, timezone, location, status and its current operational profile (operating hours, holidays, handling time, cutoffs, throughput cap). An unassigned prefix is answered as a not-found problem.",
				Tags:        []string{"topology", "service-area", "read"},
				InputModes:  []string{"application/json"},
				Examples: []string{
					`{"skill":"serving_station","arguments":{"postalPrefix":"013"}}`,
				},
			},
			{
				ID:          skillFindNodes,
				Name:        "Find network nodes",
				Description: "Lists the network's nodes by id ascending, optionally filtered by type (FULFILLMENT_CENTER, INBOUND_CROSS_DOCK, RETURNS_CENTER, SORTATION_CENTER, DELIVERY_STATION or AIR_HUB), regionId and/or status (PROPOSED, ACTIVE, SUSPENDED or RETIRED); the filters combine with AND and no match is an empty list. A type or status that is not an enumeration value is answered as a validation problem.",
				Tags:        []string{"topology", "node", "read"},
				InputModes:  []string{"application/json"},
				Examples: []string{
					`{"skill":"find_nodes","arguments":{}}`,
					`{"skill":"find_nodes","arguments":{"type":"DELIVERY_STATION","status":"ACTIVE"}}`,
				},
			},
		},
	}
}

// NewExecutor returns the AgentExecutor that maps an incoming message to a
// skill and answers with a single agent Message (a message-only agent in A2A
// terms): both curated skills are synchronous reads, so there is no
// long-running Task lifecycle to track. The response carries the caller's
// contextId (and taskId when the client referenced one).
func NewExecutor(deps Deps) a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			yield(answer(ctx, deps, ec), nil)
		}
	})
}

// Handler wires the executor into the transport-agnostic A2A request handler
// and wraps it in the JSON-RPC 2.0 HTTP handler: the only transport this
// agent ships, mirroring ADR 0008's "Streamable HTTP only" minimalism for MCP.
func Handler(executor a2asrv.AgentExecutor) http.Handler {
	return a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor))
}
