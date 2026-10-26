// Package ports declares the application's OUT ports: interfaces only
// (enforced by internal/architecture). Their non-interface vocabulary is the
// two sentinels of errors.go and the filters of internal/application/repository.
//
// Optimistic concurrency: every Save expects the stored version to be
// aggregate.Version()-1 (ADR 0002); a create (Version() 1) expects that the id
// is not stored. Any mismatch, including an update of a row that no longer
// exists, is ErrConcurrentUpdate. Inside a UnitOfWork every repository joins
// the transaction carried by the ctx the unit handed to its function.
package ports

import (
	"context"
	"time"

	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// NodeRepository persists Node aggregates and, one row per revision, their
// operational profiles (ADR 0003 decision 5).
type NodeRepository interface {
	// Get returns the node with its CURRENT profile, or ErrNotFound.
	Get(ctx context.Context, id node.ID) (*node.Node, error)
	// FindByVendorRef returns the node registered under the vendor's own
	// code, or ErrNotFound. It backs the ErrVendorNodeCodeTaken pre-check.
	FindByVendorRef(ctx context.Context, vendorID, vendorNodeCode string) (*node.Node, error)
	// List returns the nodes matching filter ordered by id ascending; no match
	// is an empty list, never an error.
	List(ctx context.Context, filter repository.NodeFilter) ([]*node.Node, error)
	// ProfileRevisions returns every stored profile revision of the node
	// ordered by revision ascending (empty when none was declared), or
	// ErrNotFound when the node does not exist.
	ProfileRevisions(ctx context.Context, id node.ID) ([]node.OperationalProfile, error)
	// Save persists the node snapshot under the concurrency rule of the
	// package comment. An adapter MUST also append the profile revision row
	// in the same transaction: when the snapshot's profile revision is higher
	// than the highest stored revision of the node it INSERTS a row keyed
	// (node id, revision) holding that profile, and it never updates or
	// deletes a stored revision (the node row points at the highest one). A
	// second node with the same (vendorId, vendorNodeCode) is
	// node.ErrVendorNodeCodeTaken (unique index).
	Save(ctx context.Context, n *node.Node) error
}

// LaneRepository persists Lane aggregates.
type LaneRepository interface {
	// Get returns the lane, or ErrNotFound.
	Get(ctx context.Context, id lane.ID) (*lane.Lane, error)
	// FindOpen returns the OPEN lane of (origin, destination, mode), or
	// ErrNotFound. It backs the ErrDuplicateLane pre-check.
	FindOpen(ctx context.Context, origin, destination node.ID, mode lane.Mode) (*lane.Lane, error)
	// List returns the lanes (open and closed) matching filter ordered by id
	// ascending; no match is an empty list.
	List(ctx context.Context, filter repository.LaneFilter) ([]*lane.Lane, error)
	// Save persists the lane under the concurrency rule of the package
	// comment. Saving an OPEN lane while another lane is OPEN for the same
	// (origin, destination, mode) is lane.ErrDuplicateLane (partial unique
	// index).
	Save(ctx context.Context, l *lane.Lane) error
}

// ServiceAreaRepository persists ServiceArea aggregates keyed by postal prefix.
type ServiceAreaRepository interface {
	// Get returns the current assignment of the prefix, or ErrNotFound. The
	// prefix is matched exactly (ADR 0002, A11).
	Get(ctx context.Context, prefix servicearea.PostalPrefix) (*servicearea.ServiceArea, error)
	// Save persists the service area under the concurrency rule of the
	// package comment.
	Save(ctx context.Context, s *servicearea.ServiceArea) error
}

// RegionRepository persists Region aggregates.
type RegionRepository interface {
	// Get returns the region, or ErrNotFound.
	Get(ctx context.Context, id region.ID) (*region.Region, error)
	// FindByCode returns the region with that code, or ErrNotFound. It backs
	// the ErrRegionCodeTaken pre-check.
	FindByCode(ctx context.Context, code string) (*region.Region, error)
	// List returns every region ordered by id ascending; none is an empty
	// list, never an error. Regions are not paginated (single digits).
	List(ctx context.Context) ([]*region.Region, error)
	// Save persists the region under the concurrency rule of the package
	// comment. A second region with the same code is region.ErrRegionCodeTaken
	// (unique index).
	Save(ctx context.Context, r *region.Region) error
}

// EventOutbox is the write side of the transactional outbox. Called with the
// ctx handed to UnitOfWork.Do it joins the SAME transaction as the aggregate
// Save calls, so the aggregates and their raised events commit or roll back
// together. The adapter encodes each event into its CloudEvents envelope (the
// id is minted once, at append) and stores one row per event in order; a relay
// drains the rows to Kafka afterwards (step 4). Appending no events is a no-op.
type EventOutbox interface {
	Append(ctx context.Context, events ...shared.Event) error
}

// UnitOfWork runs fn as ONE atomic unit: every repository and outbox call fn
// makes with the ctx it is handed commits together when fn returns nil, or
// rolls back together when fn returns an error (or panics).
//
//   - fn MUST use the ctx passed to it for every call that should participate;
//     the unit travels in that ctx.
//   - Do is re-entrant: when ctx already carries an open unit, Do joins it and
//     the outermost Do owns commit and rollback.
//   - The error fn returns is returned from Do unchanged (errors.Is keeps
//     working); a commit failure is returned wrapped.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock is the service's notion of "now"; use cases stamp aggregates with it.
type Clock interface {
	Now() time.Time
}

// IDGenerator mints the opaque ids of new aggregates (ADR 0002, A11). A
// ServiceArea has no minted id: its postal prefix is its identity.
type IDGenerator interface {
	NewNodeID() node.ID
	NewLaneID() lane.ID
	NewRegionID() region.ID
}

// NodeMetrics records the business outcome of every node registration (Tier 2
// of the fleet metrics convention, ADR 0010). RegisterNode calls it once per
// attempt: NodeRegistered when the node was stored, NodeRejected otherwise,
// with the problem slug that says why (the `type` suffix the REST adapter
// answers with: "vendor-node-code-taken", "validation-failed", ...). The
// implementation lives in internal/adapters/outbound/telemetry; a nil
// usecases.Deps.Metrics records nothing.
type NodeMetrics interface {
	// NodeRegistered counts a node that was registered.
	NodeRegistered(ctx context.Context)
	// NodeRejected counts a registration that stored no node; reason is a
	// stable, low-cardinality slug.
	NodeRejected(ctx context.Context, reason string)
}
