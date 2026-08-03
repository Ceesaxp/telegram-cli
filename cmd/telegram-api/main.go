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
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tegal1337/telegram-cli/internal/config"
	"github.com/tegal1337/telegram-cli/internal/restapi"
	"github.com/tegal1337/telegram-cli/internal/telegram"
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
		runServe(cfg, listenAddr(*addr))
	default:
		fmt.Fprintf(os.Stderr, "usage: telegram-api [login|serve] [-addr host:port]\n")
		os.Exit(2)
	}
}

// listenAddr resolves the listen address: -addr flag > TELETUI_API_ADDR
// env > default. Localhost only by default, so no auth token is needed.
func listenAddr(flagAddr string) string {
	if flagAddr != "" {
		return flagAddr
	}
	if env := os.Getenv("TELETUI_API_ADDR"); env != "" {
		return env
	}
	return "127.0.0.1:8080"
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
func runServe(cfg *config.Config, addr string) {
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

	api := restapi.New(client)
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
