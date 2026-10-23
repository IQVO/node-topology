//go:build integration

package http_test

import (
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/claudioed/node-topology/internal/testing/pgtest"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	code := m.Run()
	pgtest.Shutdown()
	os.Exit(code)
}
