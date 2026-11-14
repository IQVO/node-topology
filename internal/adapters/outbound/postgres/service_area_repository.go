package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// carrierOptionRow is the JSONB shape of one carrier option (ADR 0016).
type carrierOptionRow struct {
	CarrierID           string `json:"carrierId"`
	HandoffNodeID       string `json:"handoffNodeId"`
	CostPerPackageCents int64  `json:"costPerPackageCents"`
	TransitMinutes      int    `json:"transitMinutes"`
}

// encodeCarrierOptions renders the options as the column's JSONB; an empty
// list is the column default '[]', never NULL.
func encodeCarrierOptions(options []servicearea.CarrierOption) ([]byte, error) {
	rows := make([]carrierOptionRow, len(options))
	for i, o := range options {
		rows[i] = carrierOptionRow{
			CarrierID: o.CarrierID, HandoffNodeID: string(o.HandoffNodeID),
			CostPerPackageCents: o.CostPerPackageCents, TransitMinutes: o.TransitMinutes,
		}
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("encode carrier options: %w", err)
	}
	return b, nil
}

// decodeCarrierOptions parses the column's JSONB, refusing NULL.
func decodeCarrierOptions(b []byte) ([]servicearea.CarrierOption, error) {
	if b == nil {
		return nil, fmt.Errorf("carrier_options is NULL")
	}
	rows := []carrierOptionRow{}
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("decode carrier options: %w", err)
	}
	options := make([]servicearea.CarrierOption, len(rows))
	for i, r := range rows {
		options[i] = servicearea.CarrierOption{
			CarrierID: r.CarrierID, HandoffNodeID: node.ID(r.HandoffNodeID),
			CostPerPackageCents: r.CostPerPackageCents, TransitMinutes: r.TransitMinutes,
		}
	}
	return options, nil
}

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
		SELECT postal_prefix, delivery_station_node_id, region_id, assignment_version, assigned_at, version,
			delivery_mode, carrier_options
		FROM service_areas WHERE postal_prefix = $1`, string(prefix))
	var (
		s                      servicearea.Snapshot
		pfx, station, regionID string
		assignedAt             time.Time
		options                []byte
	)
	switch err := row.Scan(&pfx, &station, &regionID, &s.AssignmentVersion, &assignedAt, &s.Version, &s.DeliveryMode, &options); {
	case err == nil:
	case isNoRows(err):
		return nil, ports.ErrNotFound
	default:
		return nil, fmt.Errorf("read service area: %w", err)
	}
	carriers, err := decodeCarrierOptions(options)
	if err != nil {
		return nil, fmt.Errorf("read service area: %w", err)
	}
	s.PostalPrefix, s.DeliveryStationNodeID, s.RegionID = servicearea.PostalPrefix(pfx), node.ID(station), region.ID(regionID)
	s.AssignedAt, s.Carriers = assignedAt.UTC(), carriers
	return servicearea.Restore(s)
}

const (
	insertServiceArea = `INSERT INTO service_areas (postal_prefix, delivery_station_node_id, region_id, assignment_version, assigned_at, version,
		delivery_mode, carrier_options)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	updateServiceArea = `UPDATE service_areas SET delivery_station_node_id = $2, region_id = $3, assignment_version = $4,
		assigned_at = $5, version = $6, delivery_mode = $7, carrier_options = $8
		WHERE postal_prefix = $1 AND version = $6::bigint - 1`
)

// Save implements ports.ServiceAreaRepository.
func (r *ServiceAreaRepo) Save(ctx context.Context, a *servicearea.ServiceArea) error {
	s := a.Snapshot()
	options, err := encodeCarrierOptions(s.Carriers)
	if err != nil {
		return fmt.Errorf("save service area %s: %w", s.PostalPrefix, err)
	}
	err = atomically(ctx, r.pool, func(tx pgx.Tx) error {
		return upsertByVersion(ctx, tx, s.Version, "service area "+string(s.PostalPrefix), insertServiceArea, updateServiceArea,
			string(s.PostalPrefix), string(s.DeliveryStationNodeID), string(s.RegionID), s.AssignmentVersion, s.AssignedAt, s.Version,
			string(s.DeliveryMode), options)
	})
	return translateWrite(err, "save service area "+string(s.PostalPrefix), nil)
}
