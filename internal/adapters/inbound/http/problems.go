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
	idempotencyKeyReused   = problem{http.StatusConflict, "idempotency-key-reused", "Idempotency-Key was already used with a different request"}

	invalidLimit          = problem{http.StatusBadRequest, "invalid-limit", "Invalid limit"}
	invalidCursor         = problem{http.StatusBadRequest, "invalid-cursor", "Invalid cursor"}
	unknownQueryParameter = problem{http.StatusBadRequest, "unknown-query-parameter", "Unknown query parameter"}

	dependencyUnavailable = problem{http.StatusServiceUnavailable, "dependency-unavailable", "A runtime dependency is unavailable"}
)

// Typed errors the adapter raises itself, each carrying its own detail. Each is
// a function of the request alone (ADR 0020): FINAL.
var (
	errNULQuery error = detailError{"a NUL character (U+0000) is not allowed in the query string", malformedQuery, replayFinal}
	errNULBody  error = detailError{"a NUL character (U+0000) is not allowed in a request body", malformedBody, replayFinal}
	errTooLarge error = detailError{"the request body is larger than the limit", bodyTooLarge, replayFinal}
)

// adapterErrors lists the typed errors above, so the replay-class tests cover
// them with the catalogue.
var adapterErrors = []error{errNULQuery, errNULBody, errTooLarge}

// detailError is an adapter error that already knows its problem.
type detailError struct {
	detail string
	p      problem
	replay replayClass
}

func (e detailError) Error() string { return e.detail }

// replayOf returns the replay class of err, read from the same place as its
// problem: the adapter's own errors and the catalogue entries declare theirs,
// anything else (an unmapped 5xx) has none.
func replayOf(err error) replayClass {
	var de detailError
	if errors.As(err, &de) {
		return de.replay
	}
	for _, entry := range problemCatalogue {
		if errors.Is(err, entry.err) {
			return entry.replay
		}
	}
	return replayUnset
}

// problemCatalogue maps every typed error to its problem, in match order. The
// slugs are the catalogue of .claude/rules/rest-api.md: the four shared types
// of the model conventions plus one kebab-case slug per model Err.
//
// replay is the replay class of the problem (replay_class.go, ADR 0020): what
// the Idempotency-Key middleware does with the answer. Every entry declares one.
var problemCatalogue = []struct {
	err    error
	p      problem
	replay replayClass
}{
	{errMalformedRequest, malformedBody, replayFinal},
	// Shared types (model conventions.errors).
	{ports.ErrNotFound, problem{http.StatusNotFound, "not-found", "Resource not found"}, replayRetryable},
	{shared.ErrIllegalTransition, problem{http.StatusConflict, "illegal-transition", "Illegal transition"}, replayRetryable},
	{ports.ErrConcurrentUpdate, problem{http.StatusConflict, "concurrent-update", "The resource was modified by another request; re-fetch it and retry"}, replayRetryable},
	// Node.
	{node.ErrVendorNodeCodeTaken, problem{http.StatusConflict, "vendor-node-code-taken", "The vendor already registered a node with this code"}, replayFinal},
	{node.ErrStaleProfileRevision, problem{http.StatusConflict, "stale-profile-revision", "The profile revision is not newer than the current one"}, replayFinal},
	{node.ErrIncompleteOperationalProfile, problem{http.StatusConflict, "incomplete-operational-profile", "The operational profile is incomplete"}, replayRetryable},
	{node.ErrNodeRetired, problem{http.StatusConflict, "node-retired", "The node is retired"}, replayFinal},
	{node.ErrOperatorNotAllowedForNodeType, problem{http.StatusUnprocessableEntity, "operator-not-allowed-for-node-type", "This operator may not run this node type"}, replayFinal},
	{node.ErrServingStationNotAllowed, problem{http.StatusUnprocessableEntity, "serving-station-not-allowed", "A pickup point needs an active delivery station as its serving station; other node types carry none"}, replayRetryable},
	{node.ErrPointPrefixNotLabellable, problem{http.StatusUnprocessableEntity, "pickup-point-postal-code-not-labellable", "A pickup point's postal code must be uppercase letters and digits only (the label charset)"}, replayFinal},
	{node.ErrSubtypeNotAllowed, problem{http.StatusUnprocessableEntity, "subtype-not-allowed", "A subtype is not allowed for this node type"}, replayFinal},
	{node.ErrCutoffOutsideOperatingHours, problem{http.StatusUnprocessableEntity, "cutoff-outside-operating-hours", "A cutoff falls outside the operating hours of its day"}, replayFinal},
	{node.ErrSortDwellNotAllowed, problem{http.StatusUnprocessableEntity, "sort-dwell-not-allowed", "Only sorting nodes declare a sort dwell"}, replayFinal},
	{node.ErrInboundLimitsNotAllowed, problem{http.StatusUnprocessableEntity, "inbound-limits-not-allowed", "Only nodes with the INBOUND capability declare inbound limits"}, replayFinal},
	// Lane.
	{lane.ErrLaneEndpointNotActive, problem{http.StatusConflict, "lane-endpoint-not-active", "Both ends of a lane must be ACTIVE nodes"}, replayRetryable},
	{lane.ErrDuplicateLane, problem{http.StatusConflict, "duplicate-lane", "An open lane with this origin, destination and mode exists"}, replayRetryable},
	{lane.ErrLaneLoop, problem{http.StatusUnprocessableEntity, "lane-loop", "A lane cannot start and end at the same node"}, replayFinal},
	{lane.ErrLaneTypePairNotAllowed, problem{http.StatusUnprocessableEntity, "lane-type-pair-not-allowed", "This pair of node types may not be connected"}, replayFinal},
	// ServiceArea.
	{servicearea.ErrNotADeliveryStation, problem{http.StatusConflict, "not-a-delivery-station", "The node is not an ACTIVE delivery station"}, replayRetryable},
	{servicearea.ErrStaleAssignmentVersion, problem{http.StatusConflict, "stale-assignment-version", "The assignment version is not newer than the current one"}, replayFinal},
	{servicearea.ErrHandoffNodeNotUsable, problem{http.StatusConflict, "handoff-node-not-usable", "A carrier option's handoff node is not an ACTIVE delivery station or sortation centre"}, replayRetryable},
	{servicearea.ErrCarrierOptionsInvalid, problem{http.StatusUnprocessableEntity, "carrier-options-invalid", "The carrier options do not match the delivery mode"}, replayFinal},
	// Region.
	{region.ErrRegionCodeTaken, problem{http.StatusConflict, "region-code-taken", "A region with this code exists"}, replayFinal},
	{region.ErrUnknownRegion, problem{http.StatusUnprocessableEntity, "unknown-region", "The region does not exist"}, replayRetryable},
	// Any value-object rule.
	{shared.ErrValidation, problem{http.StatusUnprocessableEntity, "validation-failed", "Validation failed"}, replayFinal},
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
