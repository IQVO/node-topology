package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
)

// LaneRepo is the pgx-backed ports.LaneRepository. Inside a UnitOfWork it
// joins the transaction carried in ctx.
type LaneRepo struct {
	pool *pgxpool.Pool
}

var _ ports.LaneRepository = (*LaneRepo)(nil)

// NewLaneRepo constructs a LaneRepo over pool.
func NewLaneRepo(pool *pgxpool.Pool) *LaneRepo { return &LaneRepo{pool: pool} }

const laneColumns = `id, origin_node_id, destination_node_id, mode, nominal_transit_minutes, status, opened_at, closed_at, version`

func scanLane(row pgx.Row) (*lane.Lane, error) {
	var (
		s                lane.Snapshot
		id, origin, dest string
		mode, status     string
		openedAt         time.Time
		closedAt         *time.Time
	)
	if err := row.Scan(&id, &origin, &dest, &mode, &s.NominalTransitMinutes, &status, &openedAt, &closedAt, &s.Version); err != nil {
		return nil, err
	}
	s.ID, s.OriginNodeID, s.DestinationNodeID = lane.ID(id), node.ID(origin), node.ID(dest)
	s.Mode, s.Status = lane.Mode(mode), lane.Status(status)
	s.OpenedAt, s.ClosedAt = openedAt.UTC(), timeOrZero(closedAt)
	return lane.Restore(s)
}

func laneOrMiss(l *lane.Lane, err error) (*lane.Lane, error) {
	switch {
	case err == nil:
		return l, nil
	case isNoRows(err):
		return nil, ports.ErrNotFound
	default:
		return nil, fmt.Errorf("read lane: %w", err)
	}
}

// Get implements ports.LaneRepository.
func (r *LaneRepo) Get(ctx context.Context, id lane.ID) (*lane.Lane, error) {
	row := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT `+laneColumns+` FROM lanes WHERE id = $1`, string(id))
	return laneOrMiss(scanLane(row))
}

// FindOpen implements ports.LaneRepository.
func (r *LaneRepo) FindOpen(ctx context.Context, origin, destination node.ID, mode lane.Mode) (*lane.Lane, error) {
	row := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT `+laneColumns+`
		FROM lanes
		WHERE origin_node_id = $1 AND destination_node_id = $2 AND mode = $3 AND status = 'OPEN'`,
		string(origin), string(destination), string(mode))
	return laneOrMiss(scanLane(row))
}

// List implements ports.LaneRepository.
func (r *LaneRepo) List(ctx context.Context, f repository.LaneFilter) ([]*lane.Lane, error) {
	rows, err := queryFor(ctx, r.pool).Query(ctx, `SELECT `+laneColumns+`
		FROM lanes
		WHERE ($1::text = '' OR origin_node_id = $1) AND ($2::text = '' OR destination_node_id = $2)
		ORDER BY id COLLATE "C"`, string(f.Origin), string(f.Destination))
	if err != nil {
		return nil, fmt.Errorf("list lanes: %w", err)
	}
	defer rows.Close()
	out := []*lane.Lane{}
	for rows.Next() {
		l, err := scanLane(rows)
		if err != nil {
			return nil, fmt.Errorf("read lane: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list lanes: %w", err)
	}
	return out, nil
}

const (
	insertLane = `INSERT INTO lanes (id, origin_node_id, destination_node_id, mode, nominal_transit_minutes, status, opened_at, closed_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	updateLane = `UPDATE lanes SET origin_node_id = $2, destination_node_id = $3, mode = $4, nominal_transit_minutes = $5,
		status = $6, opened_at = $7, closed_at = $8, version = $9
		WHERE id = $1 AND version = $9::bigint - 1`
)

// Save implements ports.LaneRepository.
func (r *LaneRepo) Save(ctx context.Context, l *lane.Lane) error {
	s := l.Snapshot()
	err := atomically(ctx, r.pool, func(tx pgx.Tx) error {
		return upsertByVersion(ctx, tx, s.Version, "lane "+string(s.ID), insertLane, updateLane,
			string(s.ID), string(s.OriginNodeID), string(s.DestinationNodeID), string(s.Mode), s.NominalTransitMinutes,
			string(s.Status), s.OpenedAt, nullTime(s.ClosedAt), s.Version)
	})
	return translateWrite(err, "save lane "+string(s.ID), map[string]error{uqOneOpenLane: lane.ErrDuplicateLane})
}
