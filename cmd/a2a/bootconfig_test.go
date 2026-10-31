package main

import (
	"strings"
	"testing"
)

// A2A_AGENT_URL is mandatory and must be an absolute http(s) URL with a host:
// anything else refuses at boot, naming the variable (the chart tests grep for
// the "<VAR> is required" shape).
func TestAgentURLFromEnv_Refusals(t *testing.T) {
	tests := []struct {
		name, raw, want string
	}{
		{"unset", "", "A2A_AGENT_URL is required"},
		{"blank", "   ", "A2A_AGENT_URL is required"},
		{"relative path", "/a2a", "A2A_AGENT_URL must be an absolute http(s) URL"},
		{"host without a scheme", "node-topology-a2a:8093", "A2A_AGENT_URL must be an absolute http(s) URL"},
		{"scheme-less authority", "//node-topology-a2a:8093/", "A2A_AGENT_URL must be an absolute http(s) URL"},
		{"other scheme", "ftp://node-topology-a2a:8093/", "A2A_AGENT_URL must be an absolute http(s) URL"},
		{"no host", "http://", "A2A_AGENT_URL must be an absolute http(s) URL"},
		{"malformed", "http://[::1", "A2A_AGENT_URL must be an absolute http(s) URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := agentURLFromEnv(tt.raw)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("agentURLFromEnv(%q) = %q, %v; want an error containing %q", tt.raw, got, err, tt.want)
			}
		})
	}
}

func TestAgentURLFromEnv_Accepts(t *testing.T) {
	for _, raw := range []string{
		"http://node-topology-a2a.warehouse-systems.svc.cluster.local:8093/",
		"https://agents.example.invalid/api/a2a/node-topology",
		"http://localhost:8093",
		"  http://localhost:8093/  ",
	} {
		got, err := agentURLFromEnv(raw)
		if err != nil || got != strings.TrimSpace(raw) {
			t.Errorf("agentURLFromEnv(%q) = %q, %v; want it accepted verbatim", raw, got, err)
		}
	}
}

// run refuses a missing or malformed A2A_AGENT_URL before it touches the
// database or binds the listener: the error names the variable, and the
// DATABASE_URL error is never reached.
func TestRun_RefusesBeforeAnythingStarts(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("A2A_ADDR", "127.0.0.1:0")
	for _, raw := range []string{"", "/a2a", "ftp://x/"} {
		t.Setenv("A2A_AGENT_URL", raw)
		err := run()
		if err == nil || !strings.Contains(err.Error(), "A2A_AGENT_URL") {
			t.Fatalf("run() with A2A_AGENT_URL=%q = %v, want an error naming A2A_AGENT_URL", raw, err)
		}
		if strings.Contains(err.Error(), "DATABASE_URL") {
			t.Fatalf("run() reached the database check before validating the agent URL: %v", err)
		}
	}
}
