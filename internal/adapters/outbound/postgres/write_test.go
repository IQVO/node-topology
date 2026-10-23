package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/claudioed/node-topology/internal/application/ports"
)

// fakeTx is a pgx.Tx that records every Exec and answers canned command
// tags; the interface is a mock point by design ("to allow tests to mock
// transactions", pgx docs).
type fakeTx struct {
	pgx.Tx

	execErr error
	tags    map[string]string // SQL prefix -> command tag
	calls   []string
}

func (f *fakeTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.calls = append(f.calls, sql)
	if f.execErr != nil {
		return pgconn.CommandTag{}, f.execErr
	}
	for prefix, tag := range f.tags {
		if len(sql) >= len(prefix) && sql[:len(prefix)] == prefix {
			return pgconn.NewCommandTag(tag), nil
		}
	}
	return pgconn.NewCommandTag("OK 0"), nil
}

func TestUpsertByVersion(t *testing.T) {
	tests := []struct {
		name    string
		version int64
		tx      *fakeTx
		wantSQL string // the prefix of the one statement that must have run
		wantErr error
	}{
		{
			name:    "version 1 inserts",
			version: 1,
			tx:      &fakeTx{tags: map[string]string{"INSERT": "INSERT 0 1"}},
			wantSQL: "INSERT",
		},
		{
			name:    "an insert error is returned as is",
			version: 1,
			tx:      &fakeTx{execErr: errors.New("disk full")},
			wantErr: errors.New("disk full"),
		},
		{
			name:    "a later version updates the stored row",
			version: 3,
			tx:      &fakeTx{tags: map[string]string{"UPDATE": "UPDATE 1"}},
			wantSQL: "UPDATE",
		},
		{
			name:    "an update that touches nothing is a stale version",
			version: 3,
			tx:      &fakeTx{tags: map[string]string{"UPDATE": "UPDATE 0"}},
			wantErr: ports.ErrConcurrentUpdate,
		},
		{
			name:    "an update error is returned as is",
			version: 2,
			tx:      &fakeTx{execErr: errors.New("deadlock detected")},
			wantErr: errors.New("deadlock detected"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := upsertByVersion(context.Background(), tc.tx, tc.version, "save node N1",
				"INSERT INTO nodes", "UPDATE nodes SET version = $1", "v3", "N1")
			checkUpsertResult(t, tc.wantErr, tc.wantSQL, err, tc.tx)
		})
	}
}

func checkUpsertResult(t *testing.T, wantErr error, wantSQL string, err error, tx *fakeTx) {
	t.Helper()
	if wantErr != nil {
		if err == nil {
			t.Fatalf("upsertByVersion() = nil, want %v", wantErr)
		}
		if !errors.Is(err, wantErr) && err.Error() != wantErr.Error() {
			t.Fatalf("upsertByVersion() = %v, want %v", err, wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("upsertByVersion() = %v, want nil", err)
	}
	if len(tx.calls) != 1 {
		t.Fatalf("upsertByVersion() ran %d statements (%v), want 1", len(tx.calls), tx.calls)
	}
	if got := tx.calls[0]; got[:len(wantSQL)] != wantSQL {
		t.Fatalf("upsertByVersion() ran %q, want a statement starting with %q", got, wantSQL)
	}
}

func TestUpsertByVersionNamesTheRow(t *testing.T) {
	tx := &fakeTx{tags: map[string]string{"UPDATE": "UPDATE 0"}}
	err := upsertByVersion(context.Background(), tx, 5, "save node N1", "INSERT INTO nodes", "UPDATE nodes", "v5", "N1")
	if err == nil || !errors.Is(err, ports.ErrConcurrentUpdate) {
		t.Fatalf("upsertByVersion() = %v, want %v", err, ports.ErrConcurrentUpdate)
	}
	if want := "version 4"; !strings.Contains(err.Error(), want) {
		t.Fatalf("upsertByVersion() = %q, want it to name %q", err, want)
	}
}

func TestNullString(t *testing.T) {
	if nullString("") != nil {
		t.Fatal(`nullString("") = a pointer, want nil`)
	}
	if got := nullString("BR-SP"); got == nil || *got != "BR-SP" {
		t.Fatalf(`nullString("BR-SP") = %v, want a pointer to it`, got)
	}
}

func TestNullTime(t *testing.T) {
	if nullTime(time.Time{}) != nil {
		t.Fatal("nullTime(zero) = a pointer, want nil")
	}
	in := time.Date(2026, 10, 23, 12, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	if got := nullTime(in); got == nil || !got.Equal(in) {
		t.Fatalf("nullTime(%v) = %v, want a pointer to it", in, got)
	}
}

func TestTimeOrZero(t *testing.T) {
	if !timeOrZero(nil).IsZero() {
		t.Fatal("timeOrZero(nil) is not the zero time")
	}
	local := time.Date(2026, 10, 23, 12, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	if got := timeOrZero(&local); got != local.UTC() {
		t.Fatalf("timeOrZero(%v) = %v, want it normalised to UTC", local, got)
	}
}

func TestStringOrEmpty(t *testing.T) {
	if stringOrEmpty(nil) != "" {
		t.Fatal(`stringOrEmpty(nil) != ""`)
	}
	if got := stringOrEmpty(ptr("x")); got != "x" {
		t.Fatalf(`stringOrEmpty(&"x") = %q, want "x"`, got)
	}
}

func TestIsNoRows(t *testing.T) {
	if !isNoRows(pgx.ErrNoRows) {
		t.Fatal("isNoRows(pgx.ErrNoRows) = false, want true")
	}
	if isNoRows(errors.New("other")) {
		t.Fatal(`isNoRows("other") = true, want false`)
	}
}

func ptr(s string) *string { return &s }
