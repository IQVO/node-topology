package node

import "errors"

// The Node invariants of the model, by the model's names. Problem slugs are
// the kebab-case of the name without the Err prefix.
var (
	// ErrOperatorNotAllowedForNodeType: parcel nodes are NETWORK-operated and a
	// vendor may register only FULFILLMENT_CENTER, INBOUND_CROSS_DOCK or
	// RETURNS_CENTER nodes (422).
	ErrOperatorNotAllowedForNodeType = errors.New("operator not allowed for node type")
	// ErrVendorNodeCodeTaken: vendorNodeCode is unique per vendor (409). Raised
	// by the repository on its unique index (ADR 0002, A10).
	ErrVendorNodeCodeTaken = errors.New("vendor node code taken")
	// ErrStaleProfileRevision: a profile revision must strictly increase (409).
	ErrStaleProfileRevision = errors.New("stale profile revision")
	// ErrIncompleteOperationalProfile: a FULFILLMENT_CENTER declares at least
	// one cutoff and a handlingTime above zero before activation and keeps both
	// while ACTIVE (409).
	ErrIncompleteOperationalProfile = errors.New("incomplete operational profile")
	// ErrCutoffOutsideOperatingHours: every day of a cutoff falls inside that
	// day's operating hours (422).
	ErrCutoffOutsideOperatingHours = errors.New("cutoff outside operating hours")
	// ErrSubtypeNotAllowed: a subtype must be one of the node type's subtypes (422).
	ErrSubtypeNotAllowed = errors.New("subtype not allowed")
	// ErrSortDwellNotAllowed: sortDwellMinutes is declared only by
	// SORTATION_CENTER and AIR_HUB nodes (422).
	ErrSortDwellNotAllowed = errors.New("sort dwell not allowed")
	// ErrNodeRetired: RETIRED is terminal (409).
	ErrNodeRetired = errors.New("node retired")
)
