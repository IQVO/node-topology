// Package mcp is the inbound Model Context Protocol adapter: it exposes this
// bounded context to the AI ecosystem as a second driving adapter over the
// same application-layer read use cases the REST adapter uses. It is built on
// the official MCP Go SDK and served over Streamable HTTP only.
//
// Per docs/adr/0008-mcp-server-adoption.md the surface is READ-ONLY: every
// tool reads network-topology data (get_node, list_nodes, list_lanes,
// resolve_service_area, get_region, list_regions); registering a node, the
// profile revisions and the lifecycle transitions stay on REST and on the
// outbox, and governance_test.go fails the build on a write-verb tool name.
// This package depends inward on the application layer and the domain only,
// never on an outbound adapter or the REST adapter. The composition root
// (cmd/mcp) wires concrete repositories into the use cases. There is no auth
// of any kind (fleet-wide revert 2026-09-11).
package mcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverName and serverVersion identify this server in the MCP initialize
// handshake.
const (
	serverName    = "node-topology-mcp"
	serverVersion = "1.0.0"
)

// instructions is what an MCP client is told about this server at initialize.
const instructions = "Node Topology of the retail network (read-only): the authoritative registry of the network's nodes, their versioned " +
	"operational profiles, the lanes between them, the regions and the postal service areas. Read one node with its current profile " +
	"with get_node, or page the registry with list_nodes (optionally by type FULFILLMENT_CENTER, INBOUND_CROSS_DOCK, RETURNS_CENTER, " +
	"SORTATION_CENTER, DELIVERY_STATION or AIR_HUB, by region and by status PROPOSED, ACTIVE, SUSPENDED or RETIRED). List the directed " +
	"GROUND / AIR connections with list_lanes (optionally from one origin node and/or to one destination node; open and closed lanes " +
	"both appear, distinguished by status), and see which delivery station serves a postal prefix with resolve_service_area. Regions " +
	"partition the network: list them with list_regions or read one with get_region. Nothing here changes data: registering nodes, " +
	"declaring profile revisions, lifecycle transitions, lanes, service areas and regions are REST operations of node-topology."

// NewServer builds the MCP server for this bounded context with every tool
// registered.
func NewServer(deps Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion},
		&mcp.ServerOptions{Instructions: instructions},
	)
	deps.registerTools(server)
	return server
}

// Handler returns the Streamable HTTP handler for the MCP server.
func Handler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}
