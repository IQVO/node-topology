package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// problemSlug runs one request through h and returns the `type` of the
// problem body (empty when the response is not a problem).
func problemSlug(t *testing.T, h http.Handler, req *http.Request) (slug string, status int) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	status = rec.Code
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), contentProblem) {
		return "", status
	}
	var body problemDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem body %q: %v", rec.Body.String(), err)
	}
	return body.Type, status
}

func newRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Content-Type", contentJSON)
	return req
}

func TestKeyFrom(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		wantOK     bool
		wantStatus int
		wantSlug   string
	}{
		{name: "a missing key is a 400", key: "", wantOK: false, wantStatus: http.StatusBadRequest, wantSlug: "idempotency-key-required"},
		{name: "a normal key passes", key: "7d5cf1e2", wantOK: true},
		{name: "the longest key passes", key: strings.Repeat("k", MaxIdempotencyKeyLength), wantOK: true},
		{name: "one character more is invalid", key: strings.Repeat("k", MaxIdempotencyKeyLength+1), wantOK: false, wantStatus: http.StatusBadRequest, wantSlug: "idempotency-key-invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := newRequest(http.MethodPost, "/nodes", bytes.NewBufferString("{}"))
			if tc.key != "" {
				req.Header.Set(IdempotencyKeyHeader, tc.key)
			}
			key, ok := keyFrom(rec, req)
			checkKeyFromResult(t, tc.key, tc.wantOK, key, ok, rec)
			if tc.wantOK {
				return
			}
			checkProblem(t, rec, tc.wantStatus, tc.wantSlug)
		})
	}
}

func checkKeyFromResult(t *testing.T, wantKey string, wantOK bool, key string, ok bool, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ok != wantOK {
		t.Fatalf("keyFrom() ok = %v, want %v (response: %d %s)", ok, wantOK, rec.Code, rec.Body.String())
	}
	if wantOK && key != wantKey {
		t.Fatalf("keyFrom() = %q, want %q", key, wantKey)
	}
}

func checkProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantSlug string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d", rec.Code, wantStatus)
	}
	var body problemDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem body: %v", err)
	}
	if body.Type != wantSlug {
		t.Fatalf("problem type = %q, want %q", body.Type, wantSlug)
	}
}

func TestHasControl(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"plain-key-123", false},
		{"with space", false},
		{"with\ttab", true},
		{"with\nnewline", true},
		{"with\x00nul", true},
		{"ünïcode-á", false},
	}
	for _, tc := range tests {
		if got := hasControl(tc.in); got != tc.want {
			t.Errorf("hasControl(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFingerprint(t *testing.T) {
	base := newRequest(http.MethodPost, "/nodes", nil)
	baseHash := fingerprint(base, []byte(`{}`))
	tests := []struct {
		name   string
		mutate func() (*http.Request, []byte)
		same   bool
	}{
		{"the same request again", func() (*http.Request, []byte) { return base, []byte(`{}`) }, true},
		{"another method", func() (*http.Request, []byte) {
			return newRequest(http.MethodPut, "/nodes", nil), []byte(`{}`)
		}, false},
		{"another path", func() (*http.Request, []byte) {
			return newRequest(http.MethodPost, "/regions", nil), []byte(`{}`)
		}, false},
		{"another body", func() (*http.Request, []byte) {
			return base, []byte(`{"x":1}`)
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, body := tc.mutate()
			got := fingerprint(req, body)
			if tc.same && got != baseHash {
				t.Fatalf("fingerprint of the same request changed: %s vs %s", got, baseHash)
			}
			if !tc.same && got == baseHash {
				t.Fatalf("fingerprint collided with a different request: %s", got)
			}
		})
	}
}

func TestReplayContentType(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{http.StatusOK, contentJSON},
		{http.StatusCreated, contentJSON},
		{http.StatusSeeOther, contentJSON},
		{http.StatusBadRequest, contentProblem},
		{http.StatusUnprocessableEntity, contentProblem},
		{http.StatusInternalServerError, contentProblem},
	}
	for _, tc := range tests {
		if got := replayContentType(tc.status); got != tc.want {
			t.Errorf("replayContentType(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestCapture(t *testing.T) {
	t.Run("the first status wins", func(t *testing.T) {
		c := &capture{header: http.Header{}}
		c.WriteHeader(http.StatusCreated)
		c.WriteHeader(http.StatusTeapot)
		if c.code != http.StatusCreated {
			t.Fatalf("code = %d, want the first one (%d)", c.code, http.StatusCreated)
		}
	})
	t.Run("a write without a status is a 200", func(t *testing.T) {
		c := &capture{header: http.Header{}}
		if _, err := c.Write([]byte("hi")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if c.code != http.StatusOK {
			t.Fatalf("code = %d, want %d", c.code, http.StatusOK)
		}
		if c.body.String() != "hi" {
			t.Fatalf("body = %q, want %q", c.body.String(), "hi")
		}
	})
	t.Run("the header map is the writer's", func(t *testing.T) {
		c := &capture{header: http.Header{}}
		c.Header().Set("X-Test", "1")
		if c.header.Get("X-Test") != "1" {
			t.Fatal("Header() does not expose the capture's header map")
		}
	})
}

func TestFlush(t *testing.T) {
	rec := &capture{header: http.Header{}}
	rec.Header().Add("Content-Type", contentJSON)
	rec.Header().Add("Set-Cookie", "a=1")
	rec.Header().Add("Set-Cookie", "b=2")
	rec.WriteHeader(http.StatusCreated)
	_, _ = rec.Write([]byte(`{"ok":true}`))

	out := httptest.NewRecorder()
	flush(out, rec)
	if out.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", out.Code, http.StatusCreated)
	}
	if got := out.Header().Values("Set-Cookie"); len(got) != 2 {
		t.Fatalf("Set-Cookie = %v, want both values", got)
	}
	if out.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %q, want the captured one", out.Body.String())
	}
}

// errReader fails the moment the middleware reads it.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestKeyedRejectsBadRequestsBeforeTheStore(t *testing.T) {
	// A nil pool is safe here: every case below is rejected before the
	// middleware would open a transaction.
	handler := RequireIdempotencyKey(nil, nil)("POST /nodes")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("a request without a key is a 400 problem", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/nodes", bytes.NewBufferString("{}"))
		slug, status := problemSlug(t, handler, req)
		if status != http.StatusBadRequest || slug != "idempotency-key-required" {
			t.Fatalf("got %d %q, want 400 idempotency-key-required", status, slug)
		}
	})

	t.Run("an over-long key is a 400 problem", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/nodes", bytes.NewBufferString("{}"))
		req.Header.Set(IdempotencyKeyHeader, strings.Repeat("k", MaxIdempotencyKeyLength+1))
		slug, status := problemSlug(t, handler, req)
		if status != http.StatusBadRequest || slug != "idempotency-key-invalid" {
			t.Fatalf("got %d %q, want 400 idempotency-key-invalid", status, slug)
		}
	})

	t.Run("a body past the limit is a 413 problem", func(t *testing.T) {
		big := bytes.Repeat([]byte("x"), maxBodyBytes+1)
		req := newRequest(http.MethodPost, "/nodes", bytes.NewReader(big))
		req.Header.Set(IdempotencyKeyHeader, "k")
		slug, status := problemSlug(t, handler, req)
		if status != http.StatusRequestEntityTooLarge || slug != "request-body-too-large" {
			t.Fatalf("got %d %q, want 413 request-body-too-large", status, slug)
		}
	})

	t.Run("a body that cannot be read is a 400 problem", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/nodes", errReader{})
		req.Header.Set(IdempotencyKeyHeader, "k")
		slug, status := problemSlug(t, handler, req)
		if status != http.StatusBadRequest || slug != "malformed-request-body" {
			t.Fatalf("got %d %q, want 400 malformed-request-body", status, slug)
		}
	})
}
