package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// upsertByVersion inserts the row when version is 1 (a create) and updates it
// otherwise. insertSQL and updateSQL take the SAME argument list, whose
// version-th placeholder is the NEW aggregate version; updateSQL must match
// the row by id and by the previous version, so a stale or vanished row
// updates nothing and is reported as errStaleVersion. what names the row for
// the error message.
func upsertByVersion(ctx context.Context, tx pgx.Tx, version int64, what, insertSQL, updateSQL string, args ...any) error {
	if version == 1 {
		_, err := tx.Exec(ctx, insertSQL, args...)
		return err
	}
	tag, err := tx.Exec(ctx, updateSQL, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errStaleVersion(what, version-1)
	}
	return nil
}

// nullString maps "" to NULL.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullTime maps the zero time to NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// timeOrZero maps NULL to the zero time and normalises to UTC.
func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

// stringOrEmpty maps NULL to "".
func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// isNoRows reports whether err is pgx's miss of a QueryRow.
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
