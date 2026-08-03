package restapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests use a nil Telegram client: they only exercise routing and
// request validation, which happen before any Telegram call.

func testServer() http.Handler {
	return New(nil).Handler()
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestUnknownRouteReturnsJSON404(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nope", nil)
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, req)

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
	req := httptest.NewRequest(http.MethodDelete, "/api/health", nil)
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
}

func TestSearchChatsMissingQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/search/chats", nil)
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestChatHistoryInvalidID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/chats/abc/history", nil)
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, req)

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
			req := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			testServer().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMediaMissingParams(t *testing.T) {
	for _, url := range []string{"/api/media", "/api/media?chat_id=123", "/api/media?message_id=1"} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		testServer().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", url, rec.Code)
		}
	}
}
