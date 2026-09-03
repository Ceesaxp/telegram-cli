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
