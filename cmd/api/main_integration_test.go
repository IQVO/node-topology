//go:build integration

package main

import (
	"os"
	"testing"

	"github.com/claudioed/node-topology/internal/testing/kafkatest"
	"github.com/claudioed/node-topology/internal/testing/pgtest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	kafkatest.Shutdown()
	pgtest.Shutdown()
	os.Exit(code)
}
