package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
)

func TestUniqueViolation(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantOK   bool
		wantName string
	}{
		{
			name:   "a nil error is not a violation",
			err:    nil,
			wantOK: false,
		},
		{
			name:   "a plain error is not a violation",
			err:    errors.New("connection refused"),
			wantOK: false,
		},
		{
			name:   "another SQLSTATE is not a violation",
			err:    &pgconn.PgError{Code: "23503", ConstraintName: "lanes_origin_fkey"},
			wantOK: false,
		},
		{
			name:     "a unique violation carries its constraint",
			err:      &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: uqOneOpenLane},
			wantOK:   true,
			wantName: uqOneOpenLane,
		},
		{
			name:     "a wrapped unique violation is unwrapped",
			err:      errors.Join(errors.New("write nodes"), &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: pkNodes}),
			wantOK:   true,
			wantName: pkNodes,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, ok := uniqueViolation(tc.err)
			if ok != tc.wantOK {
				t.Fatalf("uniqueViolation(%v) ok = %v, want %v", tc.err, ok, tc.wantOK)
			}
			if ok && name != tc.wantName {
				t.Fatalf("uniqueViolation(%v) constraint = %q, want %q", tc.err, name, tc.wantName)
			}
		})
	}
}

func TestTranslateWrite(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		typed      map[string]error
		wantErr    error
		wantDetail string
	}{
		{
			name:    "a nil error stays nil",
			err:     nil,
			wantErr: nil,
		},
		{
			name:    "a business unique index becomes its typed error",
			err:     &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: uqVendorRef},
			typed:   map[string]error{uqVendorRef: node.ErrVendorNodeCodeTaken},
			wantErr: node.ErrVendorNodeCodeTaken,
		},
		{
			name:    "the lane one-open index becomes the duplicate-lane error",
			err:     &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: uqOneOpenLane},
			typed:   map[string]error{uqOneOpenLane: lane.ErrDuplicateLane},
			wantErr: lane.ErrDuplicateLane,
		},
		{
			name:    "a primary-key violation is a concurrent update",
			err:     &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: pkNodes},
			wantErr: ports.ErrConcurrentUpdate,
		},
		{
			name:    "the revision primary key is a concurrent update too",
			err:     &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: pkRevisions},
			wantErr: ports.ErrConcurrentUpdate,
		},
		{
			name:       "an untyped unique index is wrapped with what the write was doing",
			err:        &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: "something_new_key"},
			wantErr:    errors.New("non-nil"),
			wantDetail: "save nodes",
		},
		{
			name:       "any other error is wrapped with what the write was doing",
			err:        errors.New("connection refused"),
			wantErr:    errors.New("non-nil"),
			wantDetail: "save nodes: connection refused",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := translateWrite(tc.err, "save nodes", tc.typed)
			if tc.wantErr == nil {
				if got != nil {
					t.Fatalf("translateWrite() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("translateWrite() = nil, want an error")
			}
			if tc.wantDetail != "" && !strings.Contains(got.Error(), tc.wantDetail) {
				t.Fatalf("translateWrite() = %q, want it to mention %q", got, tc.wantDetail)
			}
			if tc.wantDetail == "" && !errors.Is(got, tc.wantErr) {
				t.Fatalf("translateWrite() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestTranslateWriteNamesTheRow(t *testing.T) {
	got := translateWrite(&pgconn.PgError{Code: pgUniqueViolation, ConstraintName: pkLanes}, "save lane L1", nil)
	if !errors.Is(got, ports.ErrConcurrentUpdate) {
		t.Fatalf("translateWrite() = %v, want %v", got, ports.ErrConcurrentUpdate)
	}
	if !strings.Contains(got.Error(), "save lane L1") {
		t.Fatalf("translateWrite() = %q, want it to name the row", got)
	}
}

func TestErrStaleVersion(t *testing.T) {
	err := errStaleVersion("save lane L1", 7)
	if !errors.Is(err, ports.ErrConcurrentUpdate) {
		t.Fatalf("errStaleVersion() = %v, want it to wrap %v", err, ports.ErrConcurrentUpdate)
	}
	if !strings.Contains(err.Error(), "version 7") {
		t.Fatalf("errStaleVersion() = %q, want it to name the loaded version", err)
	}
}
