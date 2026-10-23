package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// RegionRepo is the pgx-backed ports.RegionRepository. Inside a UnitOfWork it
// joins the transaction carried in ctx.
type RegionRepo struct {
	pool *pgxpool.Pool
}

var _ ports.RegionRepository = (*RegionRepo)(nil)

// NewRegionRepo constructs a RegionRepo over pool.
func NewRegionRepo(pool *pgxpool.Pool) *RegionRepo { return &RegionRepo{pool: pool} }

const regionColumns = `id, code, name, defined_at, version`

func scanRegion(row pgx.Row) (*region.Region, error) {
	var (
		s         region.Snapshot
		id        string
		definedAt time.Time
	)
	if err := row.Scan(&id, &s.Code, &s.Name, &definedAt, &s.Version); err != nil {
		return nil, err
	}
	s.ID, s.DefinedAt = region.ID(id), definedAt.UTC()
	return region.Restore(s)
}

// Get implements ports.RegionRepository.
func (r *RegionRepo) Get(ctx context.Context, id region.ID) (*region.Region, error) {
	row := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT `+regionColumns+` FROM regions WHERE id = $1`, string(id))
	return regionOrMiss(scanRegion(row))
}

// FindByCode implements ports.RegionRepository.
func (r *RegionRepo) FindByCode(ctx context.Context, code string) (*region.Region, error) {
	row := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT `+regionColumns+` FROM regions WHERE code = $1`, code)
	return regionOrMiss(scanRegion(row))
}

func regionOrMiss(r *region.Region, err error) (*region.Region, error) {
	switch {
	case err == nil:
		return r, nil
	case isNoRows(err):
		return nil, ports.ErrNotFound
	default:
		return nil, fmt.Errorf("read region: %w", err)
	}
}

// List implements ports.RegionRepository.
func (r *RegionRepo) List(ctx context.Context) ([]*region.Region, error) {
	rows, err := queryFor(ctx, r.pool).Query(ctx, `SELECT `+regionColumns+` FROM regions ORDER BY id COLLATE "C"`)
	if err != nil {
		return nil, fmt.Errorf("list regions: %w", err)
	}
	defer rows.Close()
	out := []*region.Region{}
	for rows.Next() {
		g, err := scanRegion(rows)
		if err != nil {
			return nil, fmt.Errorf("read region: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list regions: %w", err)
	}
	return out, nil
}

const (
	insertRegion = `INSERT INTO regions (id, code, name, defined_at, version) VALUES ($1, $2, $3, $4, $5)`
	updateRegion = `UPDATE regions SET code = $2, name = $3, defined_at = $4, version = $5 WHERE id = $1 AND version = $5::bigint - 1`
)

// Save implements ports.RegionRepository.
func (r *RegionRepo) Save(ctx context.Context, g *region.Region) error {
	s := g.Snapshot()
	err := atomically(ctx, r.pool, func(tx pgx.Tx) error {
		return upsertByVersion(ctx, tx, s.Version, "region "+string(s.ID), insertRegion, updateRegion,
			string(s.ID), s.Code, s.Name, s.DefinedAt, s.Version)
	})
	return translateWrite(err, "save region "+string(s.ID), map[string]error{uqRegionCode: region.ErrRegionCodeTaken})
}
