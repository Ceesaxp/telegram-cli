package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

type fakeNotifySettings struct {
	settings tg.PeerNotifySettings
	err      error
	asked    tg.InputNotifyPeerClass
}

func (f *fakeNotifySettings) AccountGetNotifySettings(_ context.Context, p tg.InputNotifyPeerClass) (*tg.PeerNotifySettings, error) {
	f.asked = p
	if f.err != nil {
		return nil, f.err
	}
	return &f.settings, nil
}

// Resolving a peer says who a chat is. Whether it is muted lives in the
// account's notify settings, which is a separate call — and without it every
// chat outside the loaded dialog page looked unmuted, so the first message
// from a silenced one rang.
func TestPeerMutedReadsTheNotifySettings(t *testing.T) {
	future := int(time.Now().Add(time.Hour).Unix())
	past := int(time.Now().Add(-time.Hour).Unix())

	tests := []struct {
		name     string
		settings tg.PeerNotifySettings
		want     bool
	}{
		{"muted until later", muteUntil(future), true},
		{"a mute that has expired", muteUntil(past), false},
		{"silenced", silent(true), true},
		{"nothing set", tg.PeerNotifySettings{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeNotifySettings{settings: tt.settings}
			peer := &tg.InputPeerUser{UserID: 7, AccessHash: 3}

			if got := peerMuted(context.Background(), api, peer); got != tt.want {
				t.Errorf("muted = %v, want %v", got, tt.want)
			}
			if _, ok := api.asked.(*tg.InputNotifyPeer); !ok {
				t.Errorf("asked about %T, want a peer", api.asked)
			}
		})
	}
}

// Of the two ways to be wrong when the settings cannot be read, ringing for
// a chat that turns out to be silenced is the one a messaging client is
// allowed to pick. Assuming muted would hide messages.
func TestPeerMutedFailsOpen(t *testing.T) {
	api := &fakeNotifySettings{err: errors.New("no connection")}

	if peerMuted(context.Background(), api, &tg.InputPeerUser{UserID: 7}) {
		t.Error("a failed lookup reported the chat as muted")
	}
}

// A chat built from a peer is partial, and has to say so — the store keeps
// the unread count, the pin and the last message only when it is told this
// is not the whole picture.
//
// This is one constructor rather than a literal at each call site because a
// flag two places have to remember is a flag one of them forgets, which is
// the exact shape of the bug the flag exists to fix.
func TestAPeerDerivedChatIsFlagged(t *testing.T) {
	msg := peerChatUpdate(&Chat{ID: 7, Title: "Ana"})

	if !msg.FromPeer {
		t.Error("a chat built from a peer is not flagged as partial")
	}
	if msg.Chat == nil || msg.Chat.Title != "Ana" {
		t.Errorf("the chat did not survive: %+v", msg.Chat)
	}
}

// A dialog is the complete view, and must NOT be flagged — otherwise the
// store would merge it and a chat could never be marked read, unpinned or
// unmuted by a reload.
func TestADialogDerivedChatIsNotFlagged(t *testing.T) {
	if (ChatUpdateMsg{Chat: &Chat{ID: 7}}).FromPeer {
		t.Error("the zero value of a dialog update claims to be partial")
	}
}

func muteUntil(until int) tg.PeerNotifySettings {
	var s tg.PeerNotifySettings
	s.SetMuteUntil(until)
	return s
}

func silent(v bool) tg.PeerNotifySettings {
	var s tg.PeerNotifySettings
	s.SetSilent(v)
	return s
}
