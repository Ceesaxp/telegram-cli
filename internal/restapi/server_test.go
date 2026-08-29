package restapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests use a nil Telegram client: they only exercise the security
// middleware and request validation, which happen before any Telegram
// call. Routes that would actually call into telegram.Client (and thus
// panic on a nil client) are covered only up to the point where they
// return a validation error or an auth/host/origin/content-type
// rejection from the middleware, never reaching s.tg.

const testToken = "s3cr3t-test-token-0123456789abcdef"

// testServer returns a handler configured with testToken as the required
// bearer token.
func testServer() http.Handler {
	return New(nil, testToken).Handler()
}

// authedRequest builds a request with a valid Host, a valid Authorization
// bearer token, and (for POST) a valid Content-Type, so tests that aren't
// about the middleware itself can focus on handler/validation behavior.
func authedRequest(method, target, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r.Host = "localhost"
	r.Header.Set("Authorization", "Bearer "+testToken)
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestHealth(t *testing.T) {
	// Health is exempt from auth: no Authorization header at all.
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "localhost"
	rec := serve(testServer(), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestUnknownRouteReturnsJSON404(t *testing.T) {
	req := authedRequest(http.MethodGet, "/api/nope", "")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestUnknownMethodReturnsJSON404(t *testing.T) {
	// The catch-all JSON 404 handler also covers wrong methods on known
	// routes (Go's ServeMux would otherwise answer plain-text 405).
	req := authedRequest(http.MethodDelete, "/api/health", "")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
}

func TestSearchChatsMissingQuery(t *testing.T) {
	// Demonstrates a properly authenticated, same-origin request passes
	// the security middleware and reaches handler-level validation.
	req := authedRequest(http.MethodGet, "/api/search/chats", "")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestChatHistoryInvalidID(t *testing.T) {
	req := authedRequest(http.MethodGet, "/api/chats/abc/history", "")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSendValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"invalid JSON", `{not json`},
		{"missing chat_id", `{"text":"hi"}`},
		{"missing text", `{"chat_id":123}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := authedRequest(http.MethodPost, "/api/send", tc.body)
			rec := serve(testServer(), req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSendFileMissingPath(t *testing.T) {
	req := authedRequest(http.MethodPost, "/api/send-file", `{"chat_id":123}`)
	rec := serve(testServer(), req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestSendJSONBodyTooLarge(t *testing.T) {
	body := `{"chat_id":1,"text":"` + strings.Repeat("a", 1<<20) + `"}`
	req := authedRequest(http.MethodPost, "/api/send", body)
	rec := serve(testServer(), req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "too large") {
		t.Fatalf("expected too-large message, got %s", got)
	}
}

func TestMediaMissingParams(t *testing.T) {
	for _, url := range []string{"/api/media", "/api/media?chat_id=123", "/api/media?message_id=1"} {
		req := authedRequest(http.MethodGet, url, "")
		rec := serve(testServer(), req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", url, rec.Code)
		}
	}
}

// --- Auth middleware ---

func TestAuthMissingTokenReturns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/chats", nil)
	req.Host = "localhost"
	rec := serve(testServer(), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error"`) {
		t.Fatalf("expected JSON error body, got: %s", body)
	}
}

func TestAuthWrongTokenReturns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/chats", nil)
	req.Host = "localhost"
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// TestAuthWrongTokenSameLengthReturns401 exercises the constant-time
// comparison path where the digest inputs would otherwise be equal
// length to the real token (ConstantTimeCompare only avoids leaking
// timing when comparing equal-size buffers; here both the supplied and
// expected values are pre-hashed to a fixed 32-byte digest either way).
func TestAuthWrongTokenSameLengthReturns401(t *testing.T) {
	wrong := strings.Repeat("x", len(testToken))
	req := httptest.NewRequest(http.MethodGet, "/api/chats", nil)
	req.Host = "localhost"
	req.Header.Set("Authorization", "Bearer "+wrong)
	rec := serve(testServer(), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMalformedHeaderReturns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/chats", nil)
	req.Host = "localhost"
	req.Header.Set("Authorization", testToken) // missing "Bearer " prefix
	rec := serve(testServer(), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthDisabledWhenTokenEmpty(t *testing.T) {
	h := New(nil, "").Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/search/chats", nil)
	req.Host = "localhost"
	rec := serve(h, req)

	// No Authorization header supplied, but auth is disabled, so the
	// request should reach handler-level validation (400 for missing q),
	// not be rejected with 401.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (auth disabled, validation still runs), got %d", rec.Code)
	}
}

func TestHealthExemptFromAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "localhost"
	// Deliberately no Authorization header.
	rec := serve(testServer(), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthValidTokenPassesToHandler(t *testing.T) {
	// /api/search/chats validates its "q" parameter before ever touching
	// the Telegram client, so this is safe to exercise with a nil client.
	req := authedRequest(http.MethodGet, "/api/search/chats", "")
	rec := serve(testServer(), req)

	// Reaching handler-level validation (400 for missing q) rather than
	// 401/403/415 proves the valid token was accepted by the middleware.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (validation reached past middleware), got %d", rec.Code)
	}
}

// --- Content-Type enforcement ---

// These use a body that fails handler-level validation (missing text) so
// a request that clears the Content-Type check never reaches the nil
// Telegram client.

func TestContentTypeRequiredForPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader(`{"chat_id":1}`))
	req.Host = "localhost"
	req.Header.Set("Authorization", "Bearer "+testToken)
	// No Content-Type set.
	rec := serve(testServer(), req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rec.Code)
	}
}

func TestContentTypeWrongValueRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader(`{"chat_id":1}`))
	req.Host = "localhost"
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "text/plain")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rec.Code)
	}
}

func TestContentTypeWithCharsetAccepted(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader(`{"chat_id":1}`))
	req.Host = "localhost"
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := serve(testServer(), req)

	// Cleared the Content-Type check and reached handler-level validation
	// (400 for missing text), rather than being rejected with 415.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("application/json with charset param should be accepted (expected 400 from validation), got %d", rec.Code)
	}
}

func TestContentTypeNotEnforcedForGet(t *testing.T) {
	req := authedRequest(http.MethodGet, "/api/search/chats", "")
	// No Content-Type on a GET; should not be rejected with 415.
	rec := serve(testServer(), req)

	if rec.Code == http.StatusUnsupportedMediaType {
		t.Fatalf("GET requests should not require Content-Type, got 415")
	}
}

// --- Host validation ---

func TestHostRejectedForNonLocalhost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "evil.example.com"
	rec := serve(testServer(), req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestHostAllowedForLocalhostVariants(t *testing.T) {
	for _, host := range []string{"localhost", "localhost:9999", "127.0.0.1", "127.0.0.1:8080", "[::1]:8080"} {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Host = host
		rec := serve(testServer(), req)

		if rec.Code != http.StatusOK {
			t.Fatalf("host %q: expected 200, got %d", host, rec.Code)
		}
	}
}

func TestHostAllowedForConfiguredListenHost(t *testing.T) {
	// A specific, non-wildcard bind address is auto-allowed.
	s := New(nil, testToken)
	s.SetListenHost("192.168.1.5:8080")
	h := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "192.168.1.5:8080"
	rec := serve(h, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for configured listen host, got %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req2.Host = "other-host:8080"
	rec2 := serve(h, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unrelated host, got %d", rec2.Code)
	}
}

// TestWildcardListenHostNotAutoAllowed guards against a security
// regression: binding to a wildcard address (0.0.0.0, ::, or ":port"
// with no host at all) must NOT implicitly allow every Host header, or
// the Host check stops defending against DNS rebinding on any host that
// happens to bind wide open.
func TestWildcardListenHostNotAutoAllowed(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080"} {
		s := New(nil, testToken)
		s.SetListenHost(addr)
		h := s.Handler()

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Host = "0.0.0.0:8080"
		rec := serve(h, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("SetListenHost(%q): wildcard bind must not auto-allow Host %q, got %d", addr, req.Host, rec.Code)
		}
	}
}

// TestAddAllowedHostRejectsWildcard guards the same rule against the
// explicit opt-in path: 0.0.0.0 must never become an accepted Host value
// even if an operator passes it to --allowed-host, since a browser can
// be tricked into treating 0.0.0.0 as a synonym for localhost.
func TestAddAllowedHostRejectsWildcard(t *testing.T) {
	s := New(nil, testToken)
	s.AddAllowedHost("0.0.0.0")
	h := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "0.0.0.0:8080"
	rec := serve(h, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("0.0.0.0 must never be an allowed Host value, got %d", rec.Code)
	}
}

// TestAllowedHostFlagGrantsAccess exercises the escape hatch for
// non-loopback binds: with a wildcard listen address, a client hitting
// the server via a LAN IP is rejected unless that IP was explicitly
// added via AddAllowedHost (wired to --allowed-host in cmd/telegram-api).
func TestAllowedHostFlagGrantsAccess(t *testing.T) {
	s := New(nil, testToken)
	s.SetListenHost("0.0.0.0:8080")
	h := s.Handler()

	blocked := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	blocked.Host = "192.168.1.5:8080"
	if rec := serve(h, blocked); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 before allowlisting, got %d", rec.Code)
	}

	s.AddAllowedHost("192.168.1.5")
	allowed := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	allowed.Host = "192.168.1.5:8080"
	if rec := serve(h, allowed); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for explicitly allowed host, got %d", rec.Code)
	}
}

// TestHostForbiddenBodyIncludesAllowlist checks the 403 is
// self-diagnosing: its body names the effective Host/Origin allowlist so
// an operator (or a well-behaved client) doesn't have to guess.
func TestHostForbiddenBodyIncludesAllowlist(t *testing.T) {
	s := New(nil, testToken)
	s.AddAllowedHost("api.example.internal")
	h := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "evil.example.com"
	rec := serve(h, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"allowed_hosts"`, "localhost", "127.0.0.1", "::1", "api.example.internal"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected 403 body to mention %q, got: %s", want, body)
		}
	}
}

// TestBracketedIPv6HostWithoutPortAllowed guards against a false-negative
// 403: a Host header of "[::1]" (bracketed, no port) must still resolve
// to the always-allowed "::1", not stay bracketed and fail to match.
func TestBracketedIPv6HostWithoutPortAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "[::1]"
	rec := serve(testServer(), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for bracketed [::1] host without port, got %d", rec.Code)
	}
}

// --- Origin validation ---

func TestOriginRejectedForForeignOrigin(t *testing.T) {
	req := authedRequest(http.MethodGet, "/api/health", "")
	req.Header.Set("Origin", "https://evil.example.com")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestOriginAllowedForLocalhost(t *testing.T) {
	req := authedRequest(http.MethodGet, "/api/health", "")
	req.Header.Set("Origin", "http://localhost:5173")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOriginAbsentIsAllowed(t *testing.T) {
	// Non-browser clients (curl, server-to-server) send no Origin header
	// at all; that must not be treated as a rejection.
	req := authedRequest(http.MethodGet, "/api/health", "")
	rec := serve(testServer(), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
