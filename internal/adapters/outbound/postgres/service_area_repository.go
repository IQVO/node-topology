package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// ServiceAreaRepo is the pgx-backed ports.ServiceAreaRepository. Inside a
// UnitOfWork it joins the transaction carried in ctx.
type ServiceAreaRepo struct {
	pool *pgxpool.Pool
}

var _ ports.ServiceAreaRepository = (*ServiceAreaRepo)(nil)

// NewServiceAreaRepo constructs a ServiceAreaRepo over pool.
func NewServiceAreaRepo(pool *pgxpool.Pool) *ServiceAreaRepo { return &ServiceAreaRepo{pool: pool} }

// Get implements ports.ServiceAreaRepository. The prefix is matched exactly.
func (r *ServiceAreaRepo) Get(ctx context.Context, prefix servicearea.PostalPrefix) (*servicearea.ServiceArea, error) {
	row := queryFor(ctx, r.pool).QueryRow(ctx, `
		SELECT postal_prefix, delivery_station_node_id, region_id, assignment_version, assigned_at, version
		FROM service_areas WHERE postal_prefix = $1`, string(prefix))
	var (
		s                      servicearea.Snapshot
		pfx, station, regionID string
		assignedAt             time.Time
	)
	switch err := row.Scan(&pfx, &station, &regionID, &s.AssignmentVersion, &assignedAt, &s.Version); {
	case err == nil:
	case isNoRows(err):
		return nil, ports.ErrNotFound
	default:
		return nil, fmt.Errorf("read service area: %w", err)
	}
	s.PostalPrefix, s.DeliveryStationNodeID, s.RegionID = servicearea.PostalPrefix(pfx), node.ID(station), region.ID(regionID)
	s.AssignedAt = assignedAt.UTC()
	return servicearea.Restore(s)
}

const (
	insertServiceArea = `INSERT INTO service_areas (postal_prefix, delivery_station_node_id, region_id, assignment_version, assigned_at, version)
		VALUES ($1, $2, $3, $4, $5, $6)`
	updateServiceArea = `UPDATE service_areas SET delivery_station_node_id = $2, region_id = $3, assignment_version = $4,
		assigned_at = $5, version = $6
		WHERE postal_prefix = $1 AND version = $6::bigint - 1`
)

// Save implements ports.ServiceAreaRepository.
func (r *ServiceAreaRepo) Save(ctx context.Context, a *servicearea.ServiceArea) error {
	s := a.Snapshot()
	err := atomically(ctx, r.pool, func(tx pgx.Tx) error {
		return upsertByVersion(ctx, tx, s.Version, "service area "+string(s.PostalPrefix), insertServiceArea, updateServiceArea,
			string(s.PostalPrefix), string(s.DeliveryStationNodeID), string(s.RegionID), s.AssignmentVersion, s.AssignedAt, s.Version)
	})
	return translateWrite(err, "save service area "+string(s.PostalPrefix), nil)
}
