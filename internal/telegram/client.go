package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"

	"github.com/tegal1337/telegram-cli/internal/config"
)

// Client wraps a gotd telegram client with the app-facing API.
type Client struct {
	mu         sync.RWMutex
	client     *telegram.Client
	api        *tg.Client
	peers      *peers.Manager
	dispatcher tg.UpdateDispatcher
	config     *config.Config
	ready      chan struct{}
	cancel     context.CancelFunc
	files      *fileRegistry

	// sendMsg forwards domain events into the bubbletea program.
	// Set by the listener; nil-safe.
	sendMsg func(tea.Msg)
}

// NewClientAsync starts the gotd client in the background.
// The client blocks on authorization — call this before starting the TUI
// so the auth UI can feed credentials via the authorizer channels.
func NewClientAsync(cfg *config.Config, authorizer *TUIAuthorizer) *Client {
	os.MkdirAll(filepath.Dir(cfg.Storage.SessionFile), 0o755)
	os.MkdirAll(cfg.Storage.FilesDir, 0o755)

	dispatcher := tg.NewUpdateDispatcher()

	c := &Client{
		config:     cfg,
		ready:      make(chan struct{}),
		files:      newFileRegistry(),
		dispatcher: dispatcher,
	}

	// The update handler is wired after construction because the peers
	// manager needs the client's API handle.
	var handler telegram.UpdateHandler
	opts := telegram.Options{
		SessionStorage: &session.FileStorage{
			Path: cfg.Storage.SessionFile,
		},
		UpdateHandler: telegram.UpdateHandlerFunc(func(ctx context.Context, u tg.UpdatesClass) error {
			if handler == nil {
				return nil
			}
			return handler.Handle(ctx, u)
		}),
		Device: telegram.DeviceConfig{
			DeviceModel:    "Tele-TUI",
			SystemVersion:  "1.0.0",
			AppVersion:     "0.1.0",
			SystemLangCode: "en",
			LangCode:       "en",
		},
		OnConnectionState: func(state telegram.ConnectionState) {
			if state == telegram.ConnectionStateReady {
				c.send(ConnectionStateMsg{State: ConnectionStateReady})
			} else {
				c.send(ConnectionStateMsg{State: ConnectionStateConnecting})
			}
		},
	}

	c.client = telegram.NewClient(int(cfg.Telegram.APIID), cfg.Telegram.APIHash, opts)
	c.api = c.client.API()
	c.peers = peers.Options{}.Build(c.api)

	gaps := updates.New(updates.Config{
		Handler:      dispatcher,
		AccessHasher: c.peers,
	})
	handler = c.peers.UpdateHook(gaps)

	// Let the authorizer fetch the 2FA password hint via this client.
	authorizer.hintFunc = func(ctx context.Context) string {
		pwd, err := c.api.AccountGetPassword(ctx)
		if err != nil {
			return ""
		}
		hint, _ := pwd.GetHint()
		return hint
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	go func() {
		err := c.client.Run(ctx, func(ctx context.Context) error {
			// Retry the auth flow on failure (bad code, wrong phone, …)
			// so a typo does not kill the whole client.
			for {
				flow := auth.NewFlow(authorizer, auth.SendCodeOptions{})
				err := c.client.Auth().IfNecessary(ctx, flow)
				if err == nil {
					break
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// Headless mode: never retry, it would loop forever.
				if errors.Is(err, ErrLoginRequired) {
					return err
				}
				authorizer.notifyError(err)
			}

			// peers.Init calls users.getUsers and must run only after
			// authorization — before that it fails with 401
			// AUTH_KEY_UNREGISTERED.
			if err := c.peers.Init(ctx); err != nil {
				return fmt.Errorf("peers init: %w", err)
			}

			authorizer.notifyState(AuthStateReady, "")
			close(c.ready)

			self, err := c.peers.Self(ctx)
			if err != nil {
				return fmt.Errorf("get self: %w", err)
			}

			return gaps.Run(ctx, c.api, self.ID(), updates.AuthOptions{})
		})
		if err != nil && ctx.Err() == nil {
			if !c.IsReady() {
				authorizer.notifyError(err)
			} else {
				log.Printf("telegram client run error: %s", err)
			}
		}
		authorizer.notifyState(AuthStateClosed, "")
	}()

	return c
}

// WaitReady blocks until the client is authorized and ready.
func (c *Client) WaitReady() {
	<-c.ready
}

// IsReady returns true if the client is authorized.
func (c *Client) IsReady() bool {
	select {
	case <-c.ready:
		return true
	default:
		return false
	}
}

// Close shuts the client down.
func (c *Client) Close() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// GetMe returns the authorized user.
func (c *Client) GetMe() (*User, error) {
	users, err := c.api.UsersGetUsers(context.Background(), []tg.InputUserClass{
		&tg.InputUserSelf{},
	})
	if err != nil {
		return nil, fmt.Errorf("get me: %w", err)
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("get me: empty response")
	}
	u, ok := users[0].(*tg.User)
	if !ok {
		return nil, fmt.Errorf("get me: unexpected type %T", users[0])
	}
	return userFromTG(u), nil
}

// DataDir returns the root data directory.
func (c *Client) DataDir() string {
	return filepath.Dir(c.config.Storage.SessionFile)
}

// send forwards an event to the bubbletea program, if a listener is wired.
func (c *Client) send(msg tea.Msg) {
	c.mu.RLock()
	send := c.sendMsg
	c.mu.RUnlock()
	if send != nil {
		send(msg)
	}
}

// setMsgSink registers the event sink (called by the listener).
func (c *Client) setMsgSink(send func(tea.Msg)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sendMsg = send
}
