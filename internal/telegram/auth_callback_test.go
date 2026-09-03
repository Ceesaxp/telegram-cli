package telegram

import (
	"errors"
	"sync"
	"testing"
)

func TestAuthorizerReplaysLatestStateToLateCallback(t *testing.T) {
	a := &TUIAuthorizer{}
	a.notifyState(AuthStateWaitPassword, "the hint")

	var gotState AuthState
	var gotHint string
	a.SetStateCallback(func(state AuthState, hint string) {
		gotState, gotHint = state, hint
	})

	if gotState != AuthStateWaitPassword || gotHint != "the hint" {
		t.Fatalf("replayed (%v, %q), want (%v, %q)",
			gotState, gotHint, AuthStateWaitPassword, "the hint")
	}
}

func TestAuthorizerOrdersStateChangesDuringLateCallbackReplay(t *testing.T) {
	a := &TUIAuthorizer{}
	a.notifyState(AuthStateWaitPhone, "")

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	var states []AuthState
	go func() {
		a.SetStateCallback(func(state AuthState, _ string) {
			if state == AuthStateWaitPhone {
				close(started)
				<-release
			}
			mu.Lock()
			states = append(states, state)
			mu.Unlock()
		})
		close(done)
	}()

	<-started
	a.notifyState(AuthStateWaitCode, "")
	close(release)
	<-done
	a.notifyState(AuthStateWaitPassword, "hint")

	mu.Lock()
	defer mu.Unlock()
	want := []AuthState{AuthStateWaitPhone, AuthStateWaitCode, AuthStateWaitPassword}
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states = %v, want %v", states, want)
		}
	}
}

func TestAuthorizerReplaysPendingErrorsOnce(t *testing.T) {
	a := &TUIAuthorizer{}
	want := errors.New("login failed")
	a.notifyError(want)

	var got []error
	a.SetErrorCallback(func(err error) { got = append(got, err) })
	a.SetErrorCallback(func(err error) { got = append(got, err) })

	if len(got) != 1 || !errors.Is(got[0], want) {
		t.Fatalf("replayed errors = %v, want [%v] once", got, want)
	}
}

func TestAuthorizerCallbacksAreRaceSafe(t *testing.T) {
	a := &TUIAuthorizer{}
	const iterations = 1_000

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			a.SetStateCallback(func(AuthState, string) {})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			a.notifyState(AuthStateWaitCode, "")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			a.SetErrorCallback(func(error) {})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			a.notifyError(errors.New("retry"))
		}
	}()
	wg.Wait()
}
