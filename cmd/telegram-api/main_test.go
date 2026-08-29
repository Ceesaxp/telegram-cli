package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// validateToken must never allow an empty/blank token through except via
// the explicit --insecure-no-auth path (resolveToken short-circuits
// before ever calling validateToken in that case). These are the pure
// checks factored out of resolveToken specifically so they're testable
// without triggering resolveToken's log.Fatal / process exit.
func TestValidateTokenRejectsEmpty(t *testing.T) {
	cases := []string{"", " ", "\t\n", "   \t  "}
	for _, tok := range cases {
		if err := validateToken(tok, "TELETUI_API_TOKEN"); err == nil {
			t.Fatalf("validateToken(%q, ...) = nil, want error", tok)
		}
	}
}

func TestValidateTokenErrorNamesSource(t *testing.T) {
	err := validateToken("", "/dev/null")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if got := err.Error(); !strings.Contains(got, "/dev/null") {
		t.Fatalf("expected error to name the source %q, got: %s", "/dev/null", got)
	}
}

func TestValidateTokenAcceptsNonEmpty(t *testing.T) {
	if err := validateToken("a-real-token-value", "TELETUI_API_TOKEN"); err != nil {
		t.Fatalf("expected no error for a non-empty token, got: %v", err)
	}
}

func TestNewHTTPServerTimeouts(t *testing.T) {
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	srv := newHTTPServer("127.0.0.1:0", h)
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v, want 30s", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 10*time.Minute {
		t.Errorf("WriteTimeout = %v, want 10m", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", srv.IdleTimeout)
	}
	if srv.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q, want 127.0.0.1:0", srv.Addr)
	}
	if srv.Handler == nil {
		t.Error("Handler is nil")
	}
}
