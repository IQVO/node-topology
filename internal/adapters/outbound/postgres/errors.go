package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/claudioed/node-topology/internal/application/ports"
)

// SQLSTATE of a unique violation.
const pgUniqueViolation = "23505"

// The unique constraints and indexes the repositories translate into the
// model's typed errors (names as declared in migrations/).
const (
	pkNodes        = "nodes_pkey"
	pkRevisions    = "node_profile_revisions_pkey"
	pkLanes        = "lanes_pkey"
	pkServiceAreas = "service_areas_pkey"
	pkRegions      = "regions_pkey"
	uqVendorRef    = "nodes_vendor_ref_key"
	uqOneOpenLane  = "lanes_one_open_key"
	uqRegionCode   = "regions_code_key"
)

// uniqueViolation returns the name of the constraint that err violated, if err
// is a Postgres unique violation.
func uniqueViolation(err error) (constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return pgErr.ConstraintName, true
	}
	return "", false
}

// translateWrite maps the unique violation of a write to the typed error the
// ports promise: a primary-key violation (the id already exists) is a
// concurrent update, a business unique index is the domain's own error named
// in typed. Any other error is wrapped with what the write was doing.
func translateWrite(err error, what string, typed map[string]error) error {
	if err == nil {
		return nil
	}
	if name, ok := uniqueViolation(err); ok {
		if e, found := typed[name]; found {
			return e
		}
		switch name {
		case pkNodes, pkRevisions, pkLanes, pkServiceAreas, pkRegions:
			return fmt.Errorf("%w: %s already stored", ports.ErrConcurrentUpdate, what)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// errStaleVersion is the miss of an UPDATE ... WHERE version = n: the row has
// another version or no longer exists.
func errStaleVersion(what string, want int64) error {
	return fmt.Errorf("%w: %s is not stored at version %d", ports.ErrConcurrentUpdate, what, want)
}
