// telegram-mcp exposes the user's Telegram account as MCP tools over stdio.
//
// Subcommands:
//
//	serve  (default) run the MCP server on stdin/stdout
//	login  interactive login, writes the shared session file
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tegal1337/telegram-cli/internal/config"
	"github.com/tegal1337/telegram-cli/internal/mcpserver"
	"github.com/tegal1337/telegram-cli/internal/telegram"
)

const loginHint = "session not authorized, run 'telegram-mcp login' first"

func main() {
	// stdout is reserved for JSON-RPC — everything else goes to stderr.
	log.SetOutput(os.Stderr)
	log.SetPrefix("telegram-mcp: ")

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if cfg.Telegram.APIID == 0 || cfg.Telegram.APIHash == "" {
		log.Fatalf("missing Telegram API credentials in config (run tele-tui once first, or edit ~/.config/tele-tui/config.toml)")
	}

	switch cmd {
	case "login":
		runLogin(cfg)
	case "serve":
		runServe(cfg)
	default:
		fmt.Fprintf(os.Stderr, "usage: telegram-mcp [login|serve]\n")
		os.Exit(2)
	}
}

// runLogin performs interactive authentication in the terminal and
// writes the session file shared with the TUI.
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

// runServe runs the MCP server over stdio. The session must already be
// authorized (via the TUI or `telegram-mcp login`).
func runServe(cfg *config.Config) {
	authorizer := telegram.NewTUIAuthorizer(cfg)
	authorizer.NonInteractive = true

	client := telegram.NewClientAsync(cfg, authorizer)
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

	srv := mcpserver.New(client)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
}
