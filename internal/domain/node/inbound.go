package node

import (
	"errors"
	"fmt"
)

// MaxInboundLimit is the largest value of an inbound limit (grams or
// millimetres). A plain literal on purpose (Postgres integer ceiling).
const MaxInboundLimit = 2147483647

// CartonDims is the largest carton a node accepts, in millimetres. The three
// edges are declared together or not at all.
type CartonDims struct {
	L int
	W int
	H int
}

// InboundLimits is what a node declares about the inbound goods it receives
// (ADR 0007). Every field is optional and an absent field means "not
// declared", which a consumer treats as unchecked; at least one is set.
// Quantities are integers: grams and millimetres.
type InboundLimits struct {
	// MaxCartonWeightG is the heaviest carton the node accepts, in grams.
	MaxCartonWeightG *int
	// MaxCartonDimsMm is the largest carton the node accepts, in millimetres.
	MaxCartonDimsMm *CartonDims
	// AcceptsPallets says whether the node receives pallets.
	AcceptsPallets *bool
	// RequiresSscc says whether every inbound carton must carry an SSCC.
	RequiresSscc *bool
}

// clone returns a deep copy; nil stays nil.
func (l *InboundLimits) clone() *InboundLimits {
	if l == nil {
		return nil
	}
	c := InboundLimits{}
	if l.MaxCartonWeightG != nil {
		v := *l.MaxCartonWeightG
		c.MaxCartonWeightG = &v
	}
	if l.MaxCartonDimsMm != nil {
		v := *l.MaxCartonDimsMm
		c.MaxCartonDimsMm = &v
	}
	if l.AcceptsPallets != nil {
		v := *l.AcceptsPallets
		c.AcceptsPallets = &v
	}
	if l.RequiresSscc != nil {
		v := *l.RequiresSscc
		c.RequiresSscc = &v
	}
	return &c
}

// validate checks the invariants: something is declared, and every declared
// number is a positive integer up to MaxInboundLimit. A nil receiver is valid
// (no limits declared).
func (l *InboundLimits) validate() error {
	if l == nil {
		return nil
	}
	if l.MaxCartonWeightG == nil && l.MaxCartonDimsMm == nil && l.AcceptsPallets == nil && l.RequiresSscc == nil {
		return errors.New("inbound must declare at least one limit; omit it to declare none")
	}
	if w := l.MaxCartonWeightG; w != nil {
		if err := checkLimit("inbound.maxCartonWeightG", *w); err != nil {
			return err
		}
	}
	if d := l.MaxCartonDimsMm; d != nil {
		for _, edge := range []struct {
			name string
			v    int
		}{{"l", d.L}, {"w", d.W}, {"h", d.H}} {
			if err := checkLimit("inbound.maxCartonDimsMm."+edge.name, edge.v); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkLimit(name string, v int) error {
	if v < 1 || v > MaxInboundLimit {
		return fmt.Errorf("%s must be between 1 and %d", name, MaxInboundLimit)
	}
	return nil
}
