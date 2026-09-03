package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"

	"github.com/Ceesaxp/telegram-cli/internal/config"
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

	// dialogs is how far down the dialog list has been read. See
	// LoadMoreChats: the chat list asks for the next page when the reader
	// reaches the bottom, and the cursor stays here rather than travelling
	// to the UI as a gotd InputPeer.
	dialogs dialogPager

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	done        chan struct{}
	run         func(context.Context)
	finishOnce  sync.Once

	// stores holds the persistent update state and peer cache. Nil when
	// the state database could not be opened (or must not be opened, see
	// stateDBTarget) — gotd then falls back to in-memory storage.
	stores *stateStores

	// sendMsg forwards domain events into the bubbletea program.
	// Set by the listener; nil-safe.
	sendMsg func(tea.Msg)

	// lastConnState is replayed to the sink when it registers — the
	// connection typically reaches Ready before the listener exists.
	lastConnState ConnectionState
	hasConnState  bool

	// pendingNotices buffers warnings and errors raised before the
	// listener registered a sink. Startup degradations are all detected
	// during construction, so without this the nil-safe send would drop
	// exactly the messages the user most needs to see. Replayed by
	// setMsgSink.
	pendingNotices []tea.Msg
}

// NewClient constructs an update-receiving client without starting it.
// Register callbacks and update handlers before calling Start.
func NewClient(cfg *config.Config, authorizer *TUIAuthorizer) *Client {
	return newClient(cfg, authorizer, false)
}

// NewRPCClient constructs an RPC-only client without starting it.
// no-updates mode: the connection never subscribes to the update stream,
// so it does not compete with the TUI (or other processes sharing the
// same session) for realtime updates. Used by telegram-mcp serve.
func NewRPCClient(cfg *config.Config, authorizer *TUIAuthorizer) *Client {
	return newClient(cfg, authorizer, true)
}

// NewClientAsync is retained for callers that have nothing to register before
// startup. Interactive frontends should use NewClient and Start explicitly.
func NewClientAsync(cfg *config.Config, authorizer *TUIAuthorizer) *Client {
	c := NewClient(cfg, authorizer)
	_ = c.Start()
	return c
}

// NewRPCClientAsync is the compatibility form of NewRPCClient.
func NewRPCClientAsync(cfg *config.Config, authorizer *TUIAuthorizer) *Client {
	c := NewRPCClient(cfg, authorizer)
	_ = c.Start()
	return c
}

func newClient(cfg *config.Config, authorizer *TUIAuthorizer, noUpdates bool) *Client {
	os.MkdirAll(filepath.Dir(cfg.Storage.SessionFile), 0o700)
	os.MkdirAll(cfg.Storage.FilesDir, 0o700)

	dispatcher := tg.NewUpdateDispatcher()

	// Persistent update state must never be shared: bbolt takes an
	// exclusive file lock, and RPC-only clients run concurrently with the
	// TUI over the same data directory. Failing to open it is not fatal —
	// we simply lose gap recovery for this run.
	var stores *stateStores
	var stateDBWarning string
	if path, want := stateDBTarget(cfg, noUpdates); want {
		s, err := openStateStores(path, stateIdentity(cfg, path))
		if err != nil {
			log.Printf("state db unavailable, continuing with in-memory update state "+
				"(offline messages will not be gap-recovered this run): %s", err)
			// Reported to the UI below, once the client exists. bbolt
			// takes an exclusive lock, so a second instance is much the
			// likeliest cause.
			stateDBWarning = "update state DB unavailable, likely locked by another " +
				"process — offline gap recovery disabled this run"
		} else {
			stores = s
		}
	}

	c := &Client{
		config:     cfg,
		ready:      make(chan struct{}),
		done:       make(chan struct{}),
		files:      newFileRegistry(),
		dispatcher: dispatcher,
		stores:     stores,
	}

	if stateDBWarning != "" {
		c.notify(ClientWarningMsg{Text: stateDBWarning})
	}

	// The update handler is wired after construction because the peers
	// manager needs the client's API handle.
	var handler telegram.UpdateHandler
	opts := telegram.Options{
		NoUpdates: noUpdates,
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
			log.Printf("connection state: %s", state)
			c.mu.Lock()
			if state == telegram.ConnectionStateReady {
				c.lastConnState = ConnectionStateReady
			} else {
				c.lastConnState = ConnectionStateConnecting
			}
			c.hasConnState = true
			// Snapshot under the lock: reading c.lastConnState after
			// the unlock races with the next state change.
			current := c.lastConnState
			c.mu.Unlock()
			c.send(ConnectionStateMsg{State: current})
		},
	}

	c.client = telegram.NewClient(int(cfg.Telegram.APIID), cfg.Telegram.APIHash, opts)
	c.api = c.client.API()
	c.peers = peers.Options{Storage: stores.peerStorage()}.Build(c.api)

	gaps := updates.New(updates.Config{
		Handler:      dispatcher,
		AccessHasher: c.peers,
		// Nil storage means in-memory: the manager then has no state to
		// restore, fetches the current one via updates.getState and
		// starts from there, exactly as before this was persisted.
		Storage: stores.stateStorage(),
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

	c.run = func(ctx context.Context) {
		defer c.finish()
		// The state database outlives every gotd write, so close it only
		// once Run has returned (i.e. after Close cancelled the context).
		defer func() {
			if err := c.stores.Close(); err != nil {
				log.Printf("closing state db: %s", err)
			}
		}()

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

			// The peer namespace is keyed by session file, but the same
			// session file can be re-authorized as a different account.
			// Now that the self ID is known, confirm the namespace really
			// belongs to it — access hashes are per-account and serving
			// stale ones yields PEER_ID_INVALID. Never fatal: a failed
			// check only costs us the cache.
			if c.stores != nil {
				if self, err := c.peers.Self(ctx); err != nil {
					log.Printf("state db: cannot confirm account identity: %s", err)
				} else if dropped, err := c.stores.bindOwner(ctx, self.ID()); err != nil {
					log.Printf("state db: owner check failed: %s", err)
				} else if dropped {
					log.Printf("state db: peer cache belonged to a different account, dropped")
					c.notify(ClientWarningMsg{
						Text: "peer cache belonged to a different account — rebuilt",
					})
				}
			}

			authorizer.notifyState(AuthStateReady, "")
			close(c.ready)

			// The connection is up and we are authorized — this is the
			// strongest "connected" signal there is, so report it
			// ourselves instead of relying solely on OnConnectionState.
			c.mu.Lock()
			c.lastConnState = ConnectionStateReady
			c.hasConnState = true
			c.mu.Unlock()
			c.send(ConnectionStateMsg{State: ConnectionStateReady})

			if noUpdates {
				// RPC-only mode: nothing to synchronize, just keep the
				// connection alive until shutdown.
				<-ctx.Done()
				return ctx.Err()
			}

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

		// Run has returned, so the client is dead whatever the reason.
		// Say so: previously this path only logged, and with the TUI
		// discarding logs a client that died AFTER connecting left the
		// status bar reading "connected" while nothing arrived. Record
		// the state too, so a sink registering later replays the truth.
		c.mu.Lock()
		c.lastConnState = ConnectionStateDisconnected
		c.hasConnState = true
		c.mu.Unlock()
		c.send(ConnectionStateMsg{State: ConnectionStateDisconnected})

		// ctx.Err() != nil means our own Close() brought it down, which
		// needs no error report. Anything else is the client dying on us
		// — session revoked from another device, a network failure gotd
		// gave up on, and so on.
		if ctx.Err() == nil {
			runErr := err
			if runErr == nil {
				runErr = errors.New("telegram client stopped unexpectedly")
			}
			c.notify(ClientErrorMsg{Err: runErr, Terminal: true})
		}

		authorizer.notifyState(AuthStateClosed, "")
	}

	return c
}

var (
	ErrClientStarted = errors.New("telegram client already started")
	ErrClientClosed  = errors.New("telegram client already closed")
)

// registerUpdateHandlers installs handlers while the dispatcher is still
// private to the constructing goroutine. The gotd dispatcher is a plain map,
// so mutation after Start would race update delivery.
func (c *Client) registerUpdateHandlers(register func(tg.UpdateDispatcher)) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.closed {
		return ErrClientClosed
	}
	if c.started {
		return ErrClientStarted
	}
	register(c.dispatcher)
	return nil
}

// Start begins authentication and update delivery after all callbacks and
// update handlers have been registered.
func (c *Client) Start() error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.closed {
		return ErrClientClosed
	}
	if c.started {
		return ErrClientStarted
	}
	c.started = true
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.run(ctx)
	return nil
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

const closeTimeout = 5 * time.Second

// Close shuts the client down and waits for its run loop and state-store
// cleanup to finish, up to a short process-shutdown bound.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	return c.CloseContext(ctx)
}

// CloseContext is Close with a caller-provided shutdown bound.
func (c *Client) CloseContext(ctx context.Context) error {
	c.lifecycleMu.Lock()
	if c.done == nil {
		c.done = make(chan struct{})
	}
	done := c.done
	if !c.started {
		if c.closed {
			c.lifecycleMu.Unlock()
			return waitForClientDone(ctx, done)
		}
		c.closed = true
		stores := c.stores
		c.stores = nil
		c.lifecycleMu.Unlock()
		if stores != nil {
			if err := stores.Close(); err != nil {
				log.Printf("closing state db: %s", err)
			}
		}
		c.finish()
		return nil
	}
	if !c.closed {
		c.closed = true
		c.cancel()
	}
	c.lifecycleMu.Unlock()
	return waitForClientDone(ctx, done)
}

func (c *Client) finish() {
	c.finishOnce.Do(func() { close(c.done) })
}

func waitForClientDone(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("telegram client shutdown: %w", ctx.Err())
	}
}

// FilesDir is the local directory downloaded media is stored in.
func (c *Client) FilesDir() string {
	return c.config.Storage.FilesDir
}

// opTimeout bounds a single RPC round-trip. Without it a hung network
// call wedges its caller (and the UI command that waits on it) forever.
const opTimeout = 30 * time.Second

// transferTimeout bounds uploads and downloads, which are chunked and
// legitimately slow for large files.
const transferTimeout = 10 * time.Minute

// opCtx returns a deadline-bound context for a short RPC.
// The caller must always defer the returned cancel.
func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

// transferCtx returns a deadline-bound context for a file transfer.
// The caller must always defer the returned cancel.
func transferCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), transferTimeout)
}

// GetMe returns the authorized user.
func (c *Client) GetMe() (*User, error) {
	ctx, cancel := opCtx()
	defer cancel()
	users, err := c.api.UsersGetUsers(ctx, []tg.InputUserClass{
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

// notify forwards a warning or error to the bubbletea program, buffering
// it when no sink has registered yet. Unlike send, which drops events
// that predate the listener, notices are kept and replayed: they are
// one-shot and mostly occur during startup.
func (c *Client) notify(msg tea.Msg) {
	c.mu.Lock()
	send := c.sendMsg
	if send == nil {
		c.pendingNotices = append(c.pendingNotices, msg)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	send(msg)
}

// setMsgSink registers the event sink (called by the listener).
func (c *Client) setMsgSink(send func(tea.Msg)) {
	c.mu.Lock()
	c.sendMsg = send
	state, has := c.lastConnState, c.hasConnState
	// Only drain the buffer if there is somewhere to drain it to.
	var pending []tea.Msg
	if send != nil {
		pending, c.pendingNotices = c.pendingNotices, nil
	}
	c.mu.Unlock()

	if send == nil {
		return
	}
	// Replay the connection state — it is usually set before the sink
	// registers, and without this the UI would stay "Disconnected".
	if has {
		send(ConnectionStateMsg{State: state})
	}
	// Replay notices raised before the TUI existed (state db
	// unavailable, peer cache rebuilt, an early client death).
	for _, msg := range pending {
		send(msg)
	}
}
