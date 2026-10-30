package usecases

import (
	"context"
	"errors"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// rejectionReasons maps a RegisterNode failure to the problem slug the REST
// adapter answers with (same vocabulary as apis/openapi.yaml), in match order.
var rejectionReasons = []struct {
	err    error
	reason string
}{
	{node.ErrVendorNodeCodeTaken, "vendor-node-code-taken"},
	{node.ErrOperatorNotAllowedForNodeType, "operator-not-allowed-for-node-type"},
	{node.ErrSubtypeNotAllowed, "subtype-not-allowed"},
	{region.ErrUnknownRegion, "unknown-region"},
	{ports.ErrConcurrentUpdate, "concurrent-update"},
	{shared.ErrValidation, "validation-failed"},
}

// internalErrorReason is the reason of every failure that is not a refusal the
// caller can act on (a database error, a bug).
const internalErrorReason = "internal-error"

// recordOutcome counts one registration attempt: registered when err is nil,
// else rejected with the reason of err.
func (uc *RegisterNode) recordOutcome(ctx context.Context, err error) {
	if err == nil {
		uc.metrics().NodeRegistered(ctx)
		return
	}
	uc.metrics().NodeRejected(ctx, rejectionReason(err))
}

func rejectionReason(err error) string {
	for _, r := range rejectionReasons {
		if errors.Is(err, r.err) {
			return r.reason
		}
	}
	return internalErrorReason
}
