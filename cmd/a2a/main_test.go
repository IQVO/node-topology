package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"

	inbounda2a "github.com/claudioed/node-topology/internal/adapters/inbound/a2a"
)

const testAgentURL = "http://node-topology-a2a.warehouse-systems.svc.cluster.local:8093/"

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func testCard() *a2a.AgentCard {
	return inbounda2a.Card(inbounda2a.Config{AgentURL: testAgentURL, Version: "test"})
}

// newTestServer serves the real A2A handler behind the real router. The zero
// Deps is enough for the transport tests: they only use requests that are
// answered before a use case runs (an unknown skill is a problem answer).
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newRouter(inbounda2a.Handler(inbounda2a.NewExecutor(inbounda2a.Deps{})), testCard(), quietLogger(), "node-topology-a2a"))
	t.Cleanup(srv.Close)
	return srv
}

func TestRouter_HealthzIsOpenAndServesOK(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != `{"status":"ok"}` {
		t.Fatalf("GET /healthz = %d %q, want 200 {\"status\":\"ok\"}", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

// The Agent Card is served, open, at the well-known path and advertises the
// configured URL and the two read skills.
func TestRouter_ServesTheAgentCardAtTheWellKnownPath(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET agent card = %d, want 200", resp.StatusCode)
	}
	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("card is not JSON: %v", err)
	}
	if card.Name != "node-topology" || len(card.Skills) != 2 || len(card.SupportedInterfaces) != 1 ||
		card.SupportedInterfaces[0].URL != testAgentURL {
		t.Fatalf("card = %+v", card)
	}
}

// Both mount points speak JSON-RPC with no credentials, driven by a real A2A
// client resolved from a card pointing at that mount point.
func TestRouter_RootAndA2APathsServeJSONRPC(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/", "/a2a"} {
		t.Run(path, func(t *testing.T) {
			card := inbounda2a.Card(inbounda2a.Config{AgentURL: srv.URL + path, Version: "test"})
			client, err := a2aclient.NewFromCard(context.Background(), card)
			if err != nil {
				t.Fatalf("NewFromCard: %v", err)
			}
			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"skill": "no_such_skill"}))
			resp, err := client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
			if err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			reply, ok := resp.(*a2a.Message)
			if !ok || len(reply.Parts) < 2 {
				t.Fatalf("reply = %T", resp)
			}
			data, _ := reply.Parts[0].Content.(a2a.Data)
			raw, _ := json.Marshal(data.Value)
			if !strings.Contains(string(raw), `"unknown-skill"`) {
				t.Fatalf("answer = %s, want the in-band unknown-skill problem", raw)
			}
		})
	}
}

// The binary reads no credential: requests carrying (or lacking) any
// Authorization header behave identically, and the card declares no scheme.
func TestNoAuthIsRequired(t *testing.T) {
	srv := newTestServer(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"data":{"skill":"no_such_skill"}}]}}}`
	for _, auth := range []string{"", "Bearer nonsense"} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("A2A-Version", string(a2a.Version))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("SendMessage with Authorization=%q = %d, want 200", auth, resp.StatusCode)
		}
	}
	if testCard().SecuritySchemes != nil {
		t.Fatal("the card must declare no security scheme")
	}
}

// A missing DATABASE_URL is a boot error, never a silent in-memory fallback.
func TestBuildDeps_MissingDatabaseFailsBoot(t *testing.T) {
	_, _, err := buildDeps(context.Background(), quietLogger(), "", "")
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("buildDeps without a database = %v, want an error naming DATABASE_URL", err)
	}
}

// An unreachable database is a boot error after the bounded retries.
func TestBuildDeps_BadDatabaseFailsBoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a cancelled context makes bootretry give up at once
	if _, _, err := buildDeps(ctx, quietLogger(), "postgres://u@127.0.0.1:1/db", "postgres://u@127.0.0.1:1/db"); err == nil {
		t.Fatal("an unreachable database must fail boot")
	}
}

func TestGetenv(t *testing.T) {
	t.Setenv("NT_A2A_TEST_SET", "v")
	t.Setenv("NT_A2A_TEST_EMPTY", "")
	if got := getenv("NT_A2A_TEST_SET", "d"); got != "v" {
		t.Errorf("set: %q", got)
	}
	if got := getenv("NT_A2A_TEST_EMPTY", "d"); got != "d" {
		t.Errorf("empty: %q", got)
	}
}

func TestNewLoggerLevels(t *testing.T) {
	for level, want := range map[string]slog.Level{"debug": slog.LevelDebug, "WARN": slog.LevelWarn, "warning": slog.LevelWarn, "error": slog.LevelError, "": slog.LevelInfo, "bogus": slog.LevelInfo} {
		if got := newLogger(level); !got.Handler().Enabled(context.Background(), want) || (want > slog.LevelDebug && got.Handler().Enabled(context.Background(), want-1)) {
			t.Errorf("newLogger(%q): level %v not the minimum enabled", level, want)
		}
	}
}
