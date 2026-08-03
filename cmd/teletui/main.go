package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/imtaqin/telegram-cli/internal/app"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

func main() {
	// Optional debug log: TELETUI_DEBUG=/tmp/teletui.log make run
	if dbg := os.Getenv("TELETUI_DEBUG"); dbg != "" {
		if f, err := os.OpenFile(dbg, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
			log.SetOutput(f)
			defer f.Close()
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Telegram.APIID == 0 || cfg.Telegram.APIHash == "" {
		if err := setupWizard(cfg); err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
	}

	s := store.NewStore()
	authorizer := telegram.NewTUIAuthorizer(cfg)

	// Start Telegram client in background — it blocks on auth.
	tgClient := telegram.NewClientAsync(cfg, authorizer)

	// Create root model.
	model := app.New(cfg, tgClient, s, authorizer)

	// Create bubbletea program.
	p := tea.NewProgram(model)

	// Wire auth state changes into bubbletea via p.Send().
	authorizer.SetStateCallback(func(state telegram.AuthState, hint string) {
		p.Send(app.AuthStateChangedMsg{State: int(state), Hint: hint})
	})
	authorizer.SetErrorCallback(func(err error) {
		p.Send(app.AuthErrorMsg{Err: err})
	})

	// Once client is ready, start the update listener.
	go func() {
		tgClient.WaitReady()
		// Registers update handlers on the client's dispatcher.
		telegram.NewListener(tgClient, p)
		// Notify TUI that we're authenticated. Retry a few times — the
		// connection may still be settling right after auth.
		var me *telegram.User
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			me, err = tgClient.GetMe()
			if err == nil && me != nil {
				break
			}
			log.Printf("get me attempt %d failed: %v", attempt+1, err)
			time.Sleep(time.Second)
		}
		if me == nil {
			p.Send(app.AuthErrorMsg{Err: fmt.Errorf("failed to load account info: %w", err)})
			return
		}
		p.Send(app.AuthenticatedMsg{
			UserId:    me.ID,
			FirstName: me.FirstName,
			LastName:  me.LastName,
		})
	}()

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		tgClient.Close()
		os.Exit(0)
	}()

	if _, err := p.Run(); err != nil {
		tgClient.Close()
		log.Fatalf("Error running TUI: %v", err)
	}

	tgClient.Close()
}

func setupWizard(cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════╗")
	fmt.Println("  ║         Telegram CLI - First Run         ║")
	fmt.Println("  ╚══════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("  Get your API credentials from:")
	fmt.Println("  https://my.telegram.org/apps")
	fmt.Println()

	for {
		fmt.Print("  Enter API ID: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		id, err := strconv.Atoi(input)
		if err != nil || id <= 0 {
			fmt.Println("  Invalid API ID. Must be a number.")
			continue
		}
		cfg.Telegram.APIID = int32(id)
		break
	}

	for {
		fmt.Print("  Enter API Hash: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if len(input) < 10 {
			fmt.Println("  Invalid API Hash. Too short.")
			continue
		}
		cfg.Telegram.APIHash = input
		break
	}

	fmt.Print("  Enter phone number (optional, press Enter to skip): ")
	phone, _ := reader.ReadString('\n')
	phone = strings.TrimSpace(phone)
	if phone != "" {
		cfg.Telegram.Phone = phone
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println()
	fmt.Println("  Config saved! Starting Telegram CLI...")
	fmt.Println()

	return nil
}
