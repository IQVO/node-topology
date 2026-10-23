// Package region is the Region aggregate: a largely self-sufficient partition
// of the network. Nodes and service areas belong to exactly one region.
//
// State: DEFINED (initial), no transition (model: no rename, no removal).
// Invariant: the region code is unique (ErrRegionCodeTaken). One aggregate
// cannot see its siblings, so the error is declared here and raised by the
// repository when its unique index on code is violated (ADR 0002, A10).
package region

import (
	"errors"
	"time"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

// ErrUnknownRegion is raised by node.Register and servicearea.Assign when the
// region they name is not a defined Region (problem type unknown-region, 422).
var ErrUnknownRegion = errors.New("unknown region")

// ErrRegionCodeTaken is raised by the repository when a region with the same
// code already exists (problem type region-code-taken, 409).
var ErrRegionCodeTaken = errors.New("region code taken")

const (
	// MaxCode is the longest region code.
	MaxCode = 16
	// MaxName is the longest region name, in characters.
	MaxName = 120
)

// ID identifies a region (regionId).
type ID string

// Region is the aggregate root.
type Region struct {
	id        ID
	code      string
	name      string
	definedAt time.Time
	version   int64
}

// Snapshot is the persisted form of a region. Restore validates it.
type Snapshot struct {
	ID        ID
	Code      string
	Name      string
	DefinedAt time.Time
	Version   int64
}

// NewRegion is the input of Define.
type NewRegion struct {
	ID   ID
	Code string
	Name string
}

// Define creates a region at version 1 and raises RegionDefined.
func Define(in NewRegion, now time.Time) (*Region, []shared.Event, error) {
	r, err := build(Snapshot{ID: in.ID, Code: in.Code, Name: in.Name, DefinedAt: now, Version: 1})
	if err != nil {
		return nil, nil, shared.Invalid("%v", err)
	}
	ev := RegionDefined{
		Header:   shared.Header{Version: r.version, At: r.definedAt},
		RegionID: r.id,
		Code:     r.code,
		Name:     r.name,
	}
	return r, []shared.Event{ev}, nil
}

// Restore rebuilds a persisted region, validating every invariant.
func Restore(s Snapshot) (*Region, error) {
	r, err := build(s)
	if err != nil {
		return nil, shared.Corrupt("region %q: %v", s.ID, err)
	}
	return r, nil
}

func build(s Snapshot) (*Region, error) {
	if err := checkValues(s); err != nil {
		return nil, err
	}
	return &Region{
		id:        s.ID,
		code:      s.Code,
		name:      s.Name,
		definedAt: s.DefinedAt.UTC(),
		version:   s.Version,
	}, nil
}

func checkValues(s Snapshot) error {
	if !shared.ValidID(string(s.ID)) {
		return errors.New("regionId is not a valid id")
	}
	if !validCode(s.Code) {
		return errors.New("code must be 1 to 16 characters from A-Z 0-9 _ -")
	}
	if !shared.ValidTextN(s.Name, MaxName) {
		return errors.New("name must be non-blank, trimmed and at most 120 characters")
	}
	if s.DefinedAt.IsZero() {
		return errors.New("definedAt is required")
	}
	if s.Version < 1 {
		return errors.New("version must be at least 1")
	}
	return nil
}

func validCode(c string) bool {
	if c == "" || len(c) > MaxCode {
		return false
	}
	for i := 0; i < len(c); i++ {
		b := c[i]
		if b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-' {
			continue
		}
		return false
	}
	return true
}

// ID returns the region id.
func (r *Region) ID() ID { return r.id }

// Code returns the unique short code.
func (r *Region) Code() string { return r.code }

// Name returns the display name.
func (r *Region) Name() string { return r.name }

// DefinedAt returns when the region was defined.
func (r *Region) DefinedAt() time.Time { return r.definedAt }

// Version returns the aggregate version.
func (r *Region) Version() int64 { return r.version }

// Snapshot returns the persisted form.
func (r *Region) Snapshot() Snapshot {
	return Snapshot{ID: r.id, Code: r.code, Name: r.name, DefinedAt: r.definedAt, Version: r.version}
}
