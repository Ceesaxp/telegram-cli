package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/tg"
)

type fakeAuthorizationLister struct {
	sessions int
	err      error
}

func (f fakeAuthorizationLister) AccountGetAuthorizations(context.Context) (*tg.AccountAuthorizations, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &tg.AccountAuthorizations{
		Authorizations: make([]tg.Authorization, f.sessions),
	}, nil
}

func TestDeviceCountIsTheNumberOfSessions(t *testing.T) {
	for _, sessions := range []int{1, 2, 9} {
		got, err := deviceCount(context.Background(),
			fakeAuthorizationLister{sessions: sessions})
		if err != nil {
			t.Fatalf("%d sessions: %v", sessions, err)
		}
		if got != sessions {
			t.Errorf("count = %d, want %d", got, sessions)
		}
	}
}

// Zero on failure, and the error alongside it. The top bar reads the zero as
// "unknown" and draws nothing; a caller that wants to tell a failure from an
// empty answer has the error to do it with.
func TestDeviceCountReportsZeroOnFailure(t *testing.T) {
	got, err := deviceCount(context.Background(),
		fakeAuthorizationLister{sessions: 4, err: errors.New("rpc timed out")})

	if err == nil {
		t.Fatal("a failed lookup reported success")
	}
	if got != 0 {
		t.Errorf("a failed lookup reported %d devices", got)
	}
}

// An account always has at least the session doing the asking, so an empty
// list is a server answering something other than what was asked. It still
// reports zero rather than inventing a one — zero draws nothing, and nothing
// is the honest rendering of an answer that makes no sense.
func TestAnEmptyListIsZero(t *testing.T) {
	got, err := deviceCount(context.Background(), fakeAuthorizationLister{})
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("count = %d, want 0", got)
	}
}
