package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// NodeRepo is the pgx-backed ports.NodeRepository. Inside a UnitOfWork it
// joins the transaction carried in ctx.
//
// A node is its current snapshot in `nodes` plus one immutable row per profile
// revision in `node_profile_revisions` (a trigger refuses UPDATE and DELETE on
// those). Save writes both in one atomic step.
type NodeRepo struct {
	pool *pgxpool.Pool
}

var _ ports.NodeRepository = (*NodeRepo)(nil)

// NewNodeRepo constructs a NodeRepo over pool.
func NewNodeRepo(pool *pgxpool.Pool) *NodeRepo { return &NodeRepo{pool: pool} }

// nodeSelect reads a node with its CURRENT profile revision (the LEFT JOIN
// yields NULL profile columns while none was declared).
const nodeSelect = `SELECT n.id, n.node_type, n.subtype, n.operator, n.vendor_id, n.vendor_node_code, n.region_id,
	n.timezone, n.postal_code, n.country_code, n.lat, n.lon, n.status, n.activated_at, n.suspended_at,
	n.suspend_reason, n.retired_at, n.version, ` + profileColumns + `
	FROM nodes n
	LEFT JOIN node_profile_revisions p ON p.node_id = n.id AND p.revision = n.profile_revision`

// scanNode reads one nodeSelect row and rebuilds the aggregate.
func scanNode(row pgx.Row) (*node.Node, error) {
	var (
		s                                     node.Snapshot
		id, typ, operator, regionID, status   string
		subtype, vendorID, vendorCode, reason *string
		activated, suspended, retired         *time.Time
		prof                                  profileRow
	)
	dest := append([]any{
		&id, &typ, &subtype, &operator, &vendorID, &vendorCode, &regionID,
		&s.Timezone, &s.Location.PostalCode, &s.Location.CountryCode, &s.Location.Lat, &s.Location.Lon, &status,
		&activated, &suspended, &reason, &retired, &s.Version,
	}, prof.dest()...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	s.ID, s.Type, s.Subtype, s.Operator = node.ID(id), node.Type(typ), node.Subtype(deref(subtype)), node.Operator(operator)
	s.RegionID, s.Status, s.SuspendReason = region.ID(regionID), node.Status(status), deref(reason)
	s.ActivatedAt, s.SuspendedAt, s.RetiredAt = timeOrZero(activated), timeOrZero(suspended), timeOrZero(retired)
	if vendorID != nil && vendorCode != nil {
		s.Vendor = &node.VendorRef{VendorID: *vendorID, VendorNodeCode: *vendorCode}
	}
	p, ok, err := prof.profile()
	if err != nil {
		return nil, err
	}
	if ok {
		s.Profile = &p
	}
	return node.Restore(s)
}

func nodeOrMiss(n *node.Node, err error) (*node.Node, error) {
	switch {
	case err == nil:
		return n, nil
	case isNoRows(err):
		return nil, ports.ErrNotFound
	default:
		return nil, fmt.Errorf("read node: %w", err)
	}
}

// Get implements ports.NodeRepository.
func (r *NodeRepo) Get(ctx context.Context, id node.ID) (*node.Node, error) {
	row := queryFor(ctx, r.pool).QueryRow(ctx, nodeSelect+` WHERE n.id = $1`, string(id))
	return nodeOrMiss(scanNode(row))
}

// FindByVendorRef implements ports.NodeRepository.
func (r *NodeRepo) FindByVendorRef(ctx context.Context, vendorID, vendorNodeCode string) (*node.Node, error) {
	row := queryFor(ctx, r.pool).QueryRow(ctx,
		nodeSelect+` WHERE n.vendor_id = $1 AND n.vendor_node_code = $2`, vendorID, vendorNodeCode)
	return nodeOrMiss(scanNode(row))
}

// List implements ports.NodeRepository.
func (r *NodeRepo) List(ctx context.Context, f repository.NodeFilter) ([]*node.Node, error) {
	rows, err := queryFor(ctx, r.pool).Query(ctx, nodeSelect+`
		WHERE ($1::text = '' OR n.node_type = $1) AND ($2::text = '' OR n.region_id = $2) AND ($3::text = '' OR n.status = $3)
		ORDER BY n.id COLLATE "C"`, string(f.Type), string(f.RegionID), string(f.Status))
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()
	out := []*node.Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("read node: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return out, nil
}

// ProfileRevisions implements ports.NodeRepository.
func (r *NodeRepo) ProfileRevisions(ctx context.Context, id node.ID) ([]node.OperationalProfile, error) {
	q := queryFor(ctx, r.pool)
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nodes WHERE id = $1)`, string(id)).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check node %s: %w", id, err)
	}
	if !exists {
		return nil, ports.ErrNotFound
	}
	rows, err := q.Query(ctx, `SELECT `+profileColumns+` FROM node_profile_revisions p WHERE p.node_id = $1 ORDER BY p.revision`, string(id))
	if err != nil {
		return nil, fmt.Errorf("list profile revisions: %w", err)
	}
	defer rows.Close()
	out := []node.OperationalProfile{}
	for rows.Next() {
		var row profileRow
		if err := rows.Scan(row.dest()...); err != nil {
			return nil, fmt.Errorf("read profile revision: %w", err)
		}
		p, _, err := row.profile()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list profile revisions: %w", err)
	}
	return out, nil
}

const (
	insertNode = `INSERT INTO nodes (id, node_type, subtype, operator, vendor_id, vendor_node_code, region_id, timezone,
		postal_code, country_code, lat, lon, status, profile_revision, activated_at, suspended_at, suspend_reason, retired_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`
	updateNode = `UPDATE nodes SET node_type = $2, subtype = $3, operator = $4, vendor_id = $5, vendor_node_code = $6,
		region_id = $7, timezone = $8, postal_code = $9, country_code = $10, lat = $11, lon = $12, status = $13,
		profile_revision = $14, activated_at = $15, suspended_at = $16, suspend_reason = $17, retired_at = $18, version = $19
		WHERE id = $1 AND version = $19::bigint - 1`

	maxRevisionOf  = `SELECT COALESCE(MAX(revision), 0) FROM node_profile_revisions WHERE node_id = $1`
	insertRevision = `INSERT INTO node_profile_revisions (node_id, revision, timezone, operating_hours, holidays,
		handling_time_minutes, cutoffs, throughput_value, throughput_time_unit, throughput_unit, sort_dwell_minutes,
		inbound, declared_by, declared_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`
)

// Save implements ports.NodeRepository: the node row under its version check,
// then, when the snapshot's profile revision is higher than the highest stored
// one, that revision as a new immutable row.
func (r *NodeRepo) Save(ctx context.Context, n *node.Node) error {
	s := n.Snapshot()
	what := "node " + string(s.ID)
	err := atomically(ctx, r.pool, func(tx pgx.Tx) error {
		var vendorID, vendorCode *string
		if s.Vendor != nil {
			vendorID, vendorCode = &s.Vendor.VendorID, &s.Vendor.VendorNodeCode
		}
		var revision *int
		if s.Profile != nil {
			revision = &s.Profile.Revision
		}
		if err := upsertByVersion(ctx, tx, s.Version, what, insertNode, updateNode,
			string(s.ID), string(s.Type), nullString(string(s.Subtype)), string(s.Operator), vendorID, vendorCode,
			string(s.RegionID), s.Timezone, s.Location.PostalCode, s.Location.CountryCode, s.Location.Lat, s.Location.Lon,
			string(s.Status), revision, nullTime(s.ActivatedAt), nullTime(s.SuspendedAt), nullString(s.SuspendReason),
			nullTime(s.RetiredAt), s.Version); err != nil {
			return err
		}
		return appendRevision(ctx, tx, s)
	})
	return translateWrite(err, "save "+what, map[string]error{uqVendorRef: node.ErrVendorNodeCodeTaken})
}

// appendRevision inserts the snapshot's profile as a new row when it is newer
// than every stored revision. Earlier rows are never touched.
func appendRevision(ctx context.Context, tx pgx.Tx, s node.Snapshot) error {
	if s.Profile == nil {
		return nil
	}
	var stored int
	if err := tx.QueryRow(ctx, maxRevisionOf, string(s.ID)).Scan(&stored); err != nil {
		return fmt.Errorf("read highest profile revision: %w", err)
	}
	p := s.Profile
	if p.Revision <= stored {
		return nil
	}
	hours, err := encodeHours(p.OperatingHours)
	if err != nil {
		return fmt.Errorf("encode operating hours: %w", err)
	}
	holidays, err := json.Marshal(p.Holidays)
	if err != nil {
		return fmt.Errorf("encode holidays: %w", err)
	}
	cutoffs, err := encodeCutoffs(p.Cutoffs)
	if err != nil {
		return fmt.Errorf("encode cutoffs: %w", err)
	}
	inbound, err := encodeInbound(p.Inbound)
	if err != nil {
		return fmt.Errorf("encode inbound limits: %w", err)
	}
	if _, err := tx.Exec(ctx, insertRevision, string(s.ID), p.Revision, p.Timezone, hours, holidays,
		p.HandlingTimeMinutes, cutoffs, p.ThroughputCap.Value, string(p.ThroughputCap.TimeUnit), p.ThroughputCap.Unit,
		p.SortDwellMinutes, inbound, string(p.DeclaredBy), p.DeclaredAt); err != nil {
		return err
	}
	return nil
}
