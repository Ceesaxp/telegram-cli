// telegram-api exposes the user's Telegram account as a JSON REST API.
//
// Subcommands:
//
//	serve  (default) run the HTTP server
//	login  interactive login, writes the MCP session file
//	       (~/.local/share/tele-tui/session-mcp.json by default)
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/restapi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

const loginHint = "session not authorized, run 'telegram-api login' first"

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("telegram-api: ")

	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("telegram-api", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "", "listen address (default 127.0.0.1:8080, or TELETUI_API_ADDR)")
	tokenFile := fs.String("token-file", "", "path to a file containing the API bearer token (overrides TELETUI_API_TOKEN and the default token file)")
	insecureNoAuth := fs.Bool("insecure-no-auth", false, "DANGEROUS: disable bearer token authentication entirely")
	var allowedHosts []string
	fs.Func("allowed-host", "additional Host/Origin hostname to accept, e.g. a LAN IP (repeatable, or comma-separated); "+
		"required to accept anything but localhost/127.0.0.1/[::1] when binding to a non-loopback address like 0.0.0.0", func(v string) error {
		for _, h := range strings.Split(v, ",") {
			if h = strings.TrimSpace(h); h != "" {
				allowedHosts = append(allowedHosts, h)
			}
		}
		return nil
	})
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if cfg.Telegram.APIID == 0 || cfg.Telegram.APIHash == "" {
		log.Fatalf("missing Telegram API credentials in config (run tele-tui once first, or edit ~/.config/tele-tui/config.toml)")
	}

	// Share the MCP session by default (NOT the TUI session): sharing one
	// Telegram session between interactive clients splits realtime updates
	// between the connections. Override with TELETUI_SESSION if ever needed.
	if s := os.Getenv("TELETUI_SESSION"); s != "" {
		cfg.Storage.SessionFile = s
	} else {
		cfg.Storage.SessionFile = strings.TrimSuffix(cfg.Storage.SessionFile, ".json") + "-mcp.json"
	}

	switch cmd {
	case "login":
		runLogin(cfg)
	case "serve":
		resolvedAddr := listenAddr(*addr)
		token := resolveToken(cfg, *tokenFile, *insecureNoAuth)
		runServe(cfg, resolvedAddr, token, allowedHosts)
	default:
		fmt.Fprintf(os.Stderr, "usage: telegram-api [login|serve] [-addr host:port] [-token-file path] [-allowed-host host] [-insecure-no-auth]\n")
		os.Exit(2)
	}
}

// listenAddr resolves the listen address: -addr flag > TELETUI_API_ADDR
// env > default.
func listenAddr(flagAddr string) string {
	if flagAddr != "" {
		return flagAddr
	}
	if env := os.Getenv("TELETUI_API_ADDR"); env != "" {
		return env
	}
	return "127.0.0.1:8080"
}

// resolveToken determines the bearer token the REST API requires, or logs
// a loud warning and returns "" (auth disabled) when --insecure-no-auth is
// set. Otherwise it loads the token in priority order: --token-file flag,
// TELETUI_API_TOKEN env var, then the default token file next to the
// session file — generating and persisting a new random token there if it
// doesn't yet exist. It is fatal for the resolved token to be empty or
// all-whitespace unless --insecure-no-auth was given: an empty token
// reaching restapi.New disables authentication entirely, so a truncated
// token file, a blank env var, or /dev/null as --token-file must never
// silently fall through to serving the account unauthenticated. The
// token value itself is never logged, only where it came from.
func resolveToken(cfg *config.Config, tokenFileFlag string, insecureNoAuth bool) string {
	if insecureNoAuth {
		log.Println("WARNING: --insecure-no-auth is set. The REST API has NO AUTHENTICATION: " +
			"anyone who can reach this address can read your Telegram messages and send " +
			"messages as you. Only use this on a fully trusted, isolated network.")
		return ""
	}

	token, source := loadToken(cfg, tokenFileFlag)
	if err := validateToken(token, source); err != nil {
		log.Fatal(err)
	}
	return token
}

// validateToken reports an error if token is empty or all-whitespace,
// naming source in the message for a clear diagnostic. It performs no
// I/O and never terminates the process itself, so it's testable in
// isolation from resolveToken's log.Fatal call.
func validateToken(token, source string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("API token from %s is empty or blank; the REST API refuses to start "+
			"unauthenticated. Provide a non-empty token, or pass --insecure-no-auth to "+
			"explicitly disable authentication", source)
	}
	return nil
}

// loadToken loads the raw token value and a human-readable description of
// where it came from, in priority order: --token-file flag,
// TELETUI_API_TOKEN env var, then the default token file next to the
// session file (generating and persisting a new random token there if it
// doesn't yet exist). It does not validate non-emptiness — the caller
// does, via validateToken — but it is fatal on any I/O error, since those
// indicate misconfiguration (unreadable file, unwritable directory) that
// the operator needs to see immediately rather than silently falling
// back to an unauthenticated server.
func loadToken(cfg *config.Config, tokenFileFlag string) (token, source string) {
	if tokenFileFlag != "" {
		data, err := os.ReadFile(tokenFileFlag)
		if err != nil {
			log.Fatalf("failed to read --token-file %s: %v", tokenFileFlag, err)
		}
		log.Printf("API token loaded from %s", tokenFileFlag)
		return strings.TrimSpace(string(data)), tokenFileFlag
	}

	if env := os.Getenv("TELETUI_API_TOKEN"); env != "" {
		log.Println("API token loaded from TELETUI_API_TOKEN")
		return strings.TrimSpace(env), "TELETUI_API_TOKEN"
	}

	path := defaultTokenPath(cfg)
	if data, err := os.ReadFile(path); err == nil {
		log.Printf("API token loaded from %s", path)
		return strings.TrimSpace(string(data)), path
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("failed to read token file %s: %v", path, err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		log.Fatalf("failed to generate API token: %v", err)
	}
	token = hex.EncodeToString(buf)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Fatalf("failed to create directory for token file %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		log.Fatalf("failed to write token file %s: %v", path, err)
	}
	log.Printf("generated new API token, written to %s", path)
	return token, path
}

// defaultTokenPath returns the default location of the API bearer token
// file: next to the (already-resolved) session file.
func defaultTokenPath(cfg *config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Storage.SessionFile), "api-token")
}

// runLogin performs interactive authentication in the terminal and
// writes the session file shared with the MCP server.
func runLogin(cfg *config.Config) {
	authorizer := telegram.NewTUIAuthorizer(cfg)
	client := telegram.NewClientAsync(cfg, authorizer)
	defer client.Close()

	var mu sync.Mutex
	state := telegram.AuthStateWaitPhone

	authorizer.SetStateCallback(func(s telegram.AuthState, hint string) {
		mu.Lock()
		state = s
		mu.Unlock()

		switch s {
		case telegram.AuthStateWaitPhone:
			fmt.Fprintln(os.Stderr, "Enter phone number (with country code):")
		case telegram.AuthStateWaitCode:
			fmt.Fprintln(os.Stderr, "Enter the login code:")
		case telegram.AuthStateWaitPassword:
			if hint != "" {
				fmt.Fprintf(os.Stderr, "Enter 2FA password (hint: %s):\n", hint)
			} else {
				fmt.Fprintln(os.Stderr, "Enter 2FA password:")
			}
		}
	})
	authorizer.SetErrorCallback(func(err error) {
		fmt.Fprintf(os.Stderr, "auth error: %v (retrying)\n", err)
	})

	// Feed stdin lines into the authorizer according to the current state.
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			value := strings.TrimSpace(scanner.Text())
			if value == "" {
				continue
			}
			mu.Lock()
			s := state
			mu.Unlock()
			switch s {
			case telegram.AuthStateWaitPhone:
				authorizer.SubmitPhone(value)
			case telegram.AuthStateWaitCode:
				authorizer.SubmitCode(value)
			case telegram.AuthStateWaitPassword:
				authorizer.SubmitPassword(value)
			}
		}
	}()

	ready := make(chan struct{})
	go func() {
		client.WaitReady()
		close(ready)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-ready:
	case <-sigCh:
		os.Exit(130)
	}

	me, err := client.GetMe()
	if err != nil {
		log.Fatalf("login succeeded but GetMe failed: %v", err)
	}

	name := strings.TrimSpace(me.FirstName + " " + me.LastName)
	if me.Username != "" {
		fmt.Fprintf(os.Stderr, "Logged in as %s (@%s, id %d)\n", name, me.Username, me.ID)
	} else {
		fmt.Fprintf(os.Stderr, "Logged in as %s (id %d)\n", name, me.ID)
	}
}

// runServe runs the REST API server. The session must already be
// authorized (via `telegram-api login` or `telegram-mcp login`). The
// client runs in RPC-only mode (no update subscription) so it never
// competes with the TUI for realtime updates.
func runServe(cfg *config.Config, addr string, token string, allowedHosts []string) {
	authorizer := telegram.NewTUIAuthorizer(cfg)
	authorizer.NonInteractive = true

	client := telegram.NewRPCClientAsync(cfg, authorizer)
	defer client.Close()

	errCh := make(chan error, 1)
	authorizer.SetErrorCallback(func(err error) {
		select {
		case errCh <- err:
		default:
		}
	})

	ready := make(chan struct{})
	go func() {
		client.WaitReady()
		close(ready)
	}()

	select {
	case <-ready:
	case err := <-errCh:
		if errors.Is(err, telegram.ErrLoginRequired) {
			fmt.Fprintln(os.Stderr, loginHint)
		} else {
			fmt.Fprintf(os.Stderr, "telegram client error: %v\n", err)
		}
		os.Exit(1)
	case <-time.After(90 * time.Second):
		fmt.Fprintln(os.Stderr, loginHint)
		os.Exit(1)
	}

	api := restapi.New(client, token)
	api.SetListenHost(addr)
	for _, h := range allowedHosts {
		api.AddAllowedHost(h)
	}
	log.Printf("allowed Host/Origin values: %s", strings.Join(api.AllowedHosts(), ", "))
	srv := &http.Server{
		Addr:    addr,
		Handler: api.Handler(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Graceful shutdown.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on http://%s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server: %v", err)
	}
}
