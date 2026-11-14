package servicearea

import (
	"errors"
	"fmt"

	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// DeliveryMode says how the last mile of a service area is served: the
// network's own route, or a handoff to an external carrier at a network node
// (ADR 0016).
type DeliveryMode string

// The two delivery modes. ModeNetworkRoute is the zero value, so an area
// assigned without a mode keeps the behaviour the network always had.
const (
	ModeNetworkRoute   DeliveryMode = "NETWORK_ROUTE"
	ModeCarrierHandoff DeliveryMode = "CARRIER_HANDOFF"
)

const (
	// MaxCarriersPerArea is the largest carrier option list an area may
	// declare (the planner's ceiling, ADR 0016).
	MaxCarriersPerArea = 4
	// MaxCarrierCostCents is the declared per-package cost ceiling, the same
	// integer-cent ceiling a lane's declared cost uses (ADR 0014).
	MaxCarrierCostCents = 10000000
)

// CarrierOption is one declared external carrier of a CARRIER_HANDOFF area:
// where the network hands packages over, and what the carrier declares the
// trip costs and takes. The per-package CHOICE is computed downstream from
// this list (lowest cost, then later transit, then smallest carrierId), never
// stored here.
type CarrierOption struct {
	CarrierID           string
	HandoffNodeID       node.ID
	CostPerPackageCents int64
	TransitMinutes      int
}

var (
	// ErrHandoffNodeNotUsable: an option's handoff node is not an ACTIVE
	// DELIVERY_STATION or SORTATION_CENTER (409).
	ErrHandoffNodeNotUsable = errors.New("handoff node not usable")
	// ErrCarrierOptionsInvalid: the carrier option list does not match the
	// delivery mode, or an option is malformed (422).
	ErrCarrierOptionsInvalid = errors.New("carrier options invalid")
)

// CarrierDelivery is the carrier half of an assignment: the mode, the options
// it declares and the handoff nodes the use case loaded for those options.
// The zero value is the NETWORK_ROUTE of today (ADR 0016).
type CarrierDelivery struct {
	Mode     DeliveryMode
	Carriers []CarrierOption
	Handoffs []*node.Node
}

// normalizeMode returns the mode with the omission semantics of the wire: an
// absent mode is the NETWORK_ROUTE the network always had.
func normalizeMode(m DeliveryMode) DeliveryMode {
	if m == "" {
		return ModeNetworkRoute
	}
	return m
}

// checkDelivery validates the mode and the option list shape and returns the
// normalised mode with a private copy of the options. The handoff nodes are
// checked by checkHandoffs, which needs the loaded aggregates.
func checkDelivery(in CarrierDelivery) (DeliveryMode, []CarrierOption, error) {
	mode := normalizeMode(in.Mode)
	if mode != ModeNetworkRoute && mode != ModeCarrierHandoff {
		return "", nil, fmt.Errorf("%w: deliveryMode %q is not a delivery mode", ErrCarrierOptionsInvalid, in.Mode)
	}
	if mode == ModeNetworkRoute {
		if len(in.Carriers) > 0 {
			return "", nil, fmt.Errorf("%w: a NETWORK_ROUTE area declares no carriers", ErrCarrierOptionsInvalid)
		}
		return mode, nil, nil
	}
	if len(in.Carriers) < 1 || len(in.Carriers) > MaxCarriersPerArea {
		return "", nil, fmt.Errorf("%w: a CARRIER_HANDOFF area declares 1 to %d carriers, got %d",
			ErrCarrierOptionsInvalid, MaxCarriersPerArea, len(in.Carriers))
	}
	options := make([]CarrierOption, len(in.Carriers))
	seen := make(map[string]bool, len(in.Carriers))
	for i, o := range in.Carriers {
		switch {
		case !shared.ValidText(o.CarrierID):
			return "", nil, fmt.Errorf("%w: carrierId %q is not a trimmed non-blank string", ErrCarrierOptionsInvalid, o.CarrierID)
		case seen[o.CarrierID]:
			return "", nil, fmt.Errorf("%w: carrierId %q is declared twice", ErrCarrierOptionsInvalid, o.CarrierID)
		case !shared.ValidID(string(o.HandoffNodeID)):
			return "", nil, fmt.Errorf("%w: handoffNodeId %q is not a valid id", ErrCarrierOptionsInvalid, o.HandoffNodeID)
		case o.CostPerPackageCents < 0 || o.CostPerPackageCents > MaxCarrierCostCents:
			return "", nil, fmt.Errorf("%w: costPerPackageCents %d is out of 0..%d",
				ErrCarrierOptionsInvalid, o.CostPerPackageCents, MaxCarrierCostCents)
		case o.TransitMinutes < 1 || o.TransitMinutes > shared.MaxInt32:
			return "", nil, fmt.Errorf("%w: transitMinutes %d is out of 1..%d",
				ErrCarrierOptionsInvalid, o.TransitMinutes, shared.MaxInt32)
		}
		seen[o.CarrierID] = true
		options[i] = o
	}
	return mode, options, nil
}

// checkHandoffs proves every option's handoff node is loaded, ACTIVE and of a
// type the sort network can reach: DELIVERY_STATION or SORTATION_CENTER. Any
// other node, or an inactive one, is refused (the two carrier-collect points
// of the model; nothing ends at a pickup point by construction).
func checkHandoffs(options []CarrierOption, handoffs []*node.Node) error {
	for _, o := range options {
		usable := false
		for _, h := range handoffs {
			if h != nil && h.ID() == o.HandoffNodeID {
				usable = h.Status() == node.StatusActive &&
					(h.Type() == node.DeliveryStation || h.Type() == node.SortationCenter)
				break
			}
		}
		if !usable {
			return fmt.Errorf("%w: %s", ErrHandoffNodeNotUsable, o.HandoffNodeID)
		}
	}
	return nil
}

// checkCarrierDelivery validates a whole CarrierDelivery and returns the
// normalised mode with a private copy of the options.
func checkCarrierDelivery(in CarrierDelivery) (DeliveryMode, []CarrierOption, error) {
	mode, options, err := checkDelivery(in)
	if err != nil {
		return "", nil, err
	}
	if err := checkHandoffs(options, in.Handoffs); err != nil {
		return "", nil, err
	}
	return mode, options, nil
}

// cloneCarriers returns a private copy of the options, so a caller can never
// alias the aggregate's list.
func cloneCarriers(in []CarrierOption) []CarrierOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]CarrierOption, len(in))
	copy(out, in)
	return out
}
