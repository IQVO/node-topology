// Package http is the inbound REST adapter: a chi router implementing every
// operation of apis/openapi.yaml, the Idempotency-Key and NUL-rejection
// middleware, RFC 9457 problem+json errors and the /healthz and /readyz
// probes. It depends on the application layer only; the composition root
// (cmd/api) hands it the use cases, the readiness gate and the transaction
// binder.
package http

import (
	"errors"
	"net/http"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// errMalformedRequest marks a body or query the adapter itself rejects (bad
// JSON, an unknown field, a wrong type, a NUL character).
var errMalformedRequest = errors.New("request is not valid for this operation")

// problem is the fixed (status, slug, title) of one error category; the detail
// comes from the error at write time. The slug is the `type` of the RFC 9457
// body (apis/openapi.yaml Problem).
type problem struct {
	status int
	slug   string
	title  string
}

var (
	internalProblem = problem{http.StatusInternalServerError, "internal-error", "Internal server error"}

	routeNotFound    = problem{http.StatusNotFound, "not-found", "Resource not found"}
	methodNotAllowed = problem{http.StatusMethodNotAllowed, "method-not-allowed", "Method not allowed"}

	malformedBody  = problem{http.StatusBadRequest, "malformed-request-body", "Malformed request body"}
	malformedQuery = problem{http.StatusBadRequest, "malformed-request-query", "Malformed query string"}
	bodyTooLarge   = problem{http.StatusRequestEntityTooLarge, "request-body-too-large", "Request body too large"}

	idempotencyKeyRequired = problem{http.StatusBadRequest, "idempotency-key-required", "Idempotency-Key header is required"}
	idempotencyKeyInvalid  = problem{http.StatusBadRequest, "idempotency-key-invalid", "Idempotency-Key header is invalid"}
	idempotencyKeyReused   = problem{http.StatusUnprocessableEntity, "idempotency-key-reused", "Idempotency-Key was already used with a different request"}

	dependencyUnavailable = problem{http.StatusServiceUnavailable, "dependency-unavailable", "A runtime dependency is unavailable"}
)

// Typed errors the adapter raises itself, each carrying its own detail.
var (
	errNULQuery error = detailError{"a NUL character (U+0000) is not allowed in the query string", malformedQuery}
	errNULBody  error = detailError{"a NUL character (U+0000) is not allowed in a request body", malformedBody}
	errTooLarge error = detailError{"the request body is larger than the limit", bodyTooLarge}
)

// detailError is an adapter error that already knows its problem.
type detailError struct {
	detail string
	p      problem
}

func (e detailError) Error() string { return e.detail }

// problemCatalogue maps every typed error to its problem, in match order. The
// slugs are the catalogue of .claude/rules/rest-api.md: the four shared types
// of the model conventions plus one kebab-case slug per model Err.
var problemCatalogue = []struct {
	err error
	p   problem
}{
	{errMalformedRequest, malformedBody},
	// Shared types (model conventions.errors).
	{ports.ErrNotFound, problem{http.StatusNotFound, "not-found", "Resource not found"}},
	{shared.ErrIllegalTransition, problem{http.StatusConflict, "illegal-transition", "Illegal transition"}},
	{ports.ErrConcurrentUpdate, problem{http.StatusConflict, "concurrent-update", "The resource was modified by another request; re-fetch it and retry"}},
	// Node.
	{node.ErrVendorNodeCodeTaken, problem{http.StatusConflict, "vendor-node-code-taken", "The vendor already registered a node with this code"}},
	{node.ErrStaleProfileRevision, problem{http.StatusConflict, "stale-profile-revision", "The profile revision is not newer than the current one"}},
	{node.ErrIncompleteOperationalProfile, problem{http.StatusConflict, "incomplete-operational-profile", "The operational profile is incomplete"}},
	{node.ErrNodeRetired, problem{http.StatusConflict, "node-retired", "The node is retired"}},
	{node.ErrOperatorNotAllowedForNodeType, problem{http.StatusUnprocessableEntity, "operator-not-allowed-for-node-type", "This operator may not run this node type"}},
	{node.ErrSubtypeNotAllowed, problem{http.StatusUnprocessableEntity, "subtype-not-allowed", "A subtype is not allowed for this node type"}},
	{node.ErrCutoffOutsideOperatingHours, problem{http.StatusUnprocessableEntity, "cutoff-outside-operating-hours", "A cutoff falls outside the operating hours of its day"}},
	{node.ErrSortDwellNotAllowed, problem{http.StatusUnprocessableEntity, "sort-dwell-not-allowed", "Only sorting nodes declare a sort dwell"}},
	// Lane.
	{lane.ErrLaneEndpointNotActive, problem{http.StatusConflict, "lane-endpoint-not-active", "Both ends of a lane must be ACTIVE nodes"}},
	{lane.ErrDuplicateLane, problem{http.StatusConflict, "duplicate-lane", "An open lane with this origin, destination and mode exists"}},
	{lane.ErrLaneLoop, problem{http.StatusUnprocessableEntity, "lane-loop", "A lane cannot start and end at the same node"}},
	{lane.ErrLaneTypePairNotAllowed, problem{http.StatusUnprocessableEntity, "lane-type-pair-not-allowed", "This pair of node types may not be connected"}},
	// ServiceArea.
	{servicearea.ErrNotADeliveryStation, problem{http.StatusConflict, "not-a-delivery-station", "The node is not an ACTIVE delivery station"}},
	{servicearea.ErrStaleAssignmentVersion, problem{http.StatusConflict, "stale-assignment-version", "The assignment version is not newer than the current one"}},
	// Region.
	{region.ErrRegionCodeTaken, problem{http.StatusConflict, "region-code-taken", "A region with this code exists"}},
	{region.ErrUnknownRegion, problem{http.StatusUnprocessableEntity, "unknown-region", "The region does not exist"}},
	// Any value-object rule.
	{shared.ErrValidation, problem{http.StatusUnprocessableEntity, "validation-failed", "Validation failed"}},
}

// problemFor returns the problem of err: the adapter's own detailError first,
// then the first catalogue entry err matches, then the internal-error problem.
func problemFor(err error) problem {
	var de detailError
	if errors.As(err, &de) {
		return de.p
	}
	for _, entry := range problemCatalogue {
		if errors.Is(err, entry.err) {
			return entry.p
		}
	}
	return internalProblem
}
