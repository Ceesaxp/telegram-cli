package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/tg"
)

func lifecycleTestClient(run func(context.Context)) *Client {
	c := &Client{
		dispatcher: tg.NewUpdateDispatcher(),
		done:       make(chan struct{}),
	}
	c.run = func(ctx context.Context) {
		defer c.finish()
		run(ctx)
	}
	return c
}

func TestClientHandlersMustBeRegisteredBeforeStart(t *testing.T) {
	c := lifecycleTestClient(func(ctx context.Context) { <-ctx.Done() })
	registered := false
	if err := c.registerUpdateHandlers(func(tg.UpdateDispatcher) { registered = true }); err != nil {
		t.Fatalf("register before Start: %v", err)
	}
	if !registered {
		t.Fatal("registration hook did not run")
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.registerUpdateHandlers(func(tg.UpdateDispatcher) {}); !errors.Is(err, ErrClientStarted) {
		t.Fatalf("register after Start = %v, want ErrClientStarted", err)
	}
	if err := c.Start(); !errors.Is(err, ErrClientStarted) {
		t.Fatalf("second Start = %v, want ErrClientStarted", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNewListenerDoesNotSendBeforeProgramRun(t *testing.T) {
	c := lifecycleTestClient(func(context.Context) {})
	c.notify(ClientWarningMsg{Text: "construction warning"})
	p := tea.NewProgram(nil)

	done := make(chan error, 1)
	go func() {
		_, err := NewListener(c, p)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("NewListener: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("NewListener blocked sending to a program that has not started")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.sendMsg != nil {
		t.Fatal("NewListener attached the message sink before Start")
	}
	if len(c.pendingNotices) != 1 {
		t.Fatalf("pending notices = %d, want 1", len(c.pendingNotices))
	}
}

func TestClientCloseWaitsForRunCleanup(t *testing.T) {
	cancelled := make(chan struct{})
	releaseCleanup := make(chan struct{})
	c := lifecycleTestClient(func(ctx context.Context) {
		<-ctx.Done()
		close(cancelled)
		<-releaseCleanup
	})
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	closed := make(chan error, 1)
	go func() { closed <- c.CloseContext(context.Background()) }()
	<-cancelled
	select {
	case err := <-closed:
		t.Fatalf("Close returned before cleanup was released: %v", err)
	default:
	}

	close(releaseCleanup)
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestClientCloseHonorsCallerDeadline(t *testing.T) {
	release := make(chan struct{})
	c := lifecycleTestClient(func(ctx context.Context) {
		<-ctx.Done()
		<-release
	})
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := c.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext = %v, want context deadline", err)
	}
	close(release)
	<-c.done
}

func TestClosingUnstartedClientPreventsStart(t *testing.T) {
	c := lifecycleTestClient(func(context.Context) {})
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Start(); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("Start after Close = %v, want ErrClientClosed", err)
	}
}
