package app

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/charmbracelet/x/ansi"
)

// TestComposerModeIsExhaustive walks every InteractionMode through the
// projection onto the composer's badge enum.
//
// Two enums exist because the app imports the composer and not the other way
// round. This is the guard that keeps them from drifting: a fourth
// InteractionMode landing without a case here would map to NORMAL, and the
// badge would quietly start lying about a state nobody thought to check.
func TestComposerModeIsExhaustive(t *testing.T) {
	want := map[InteractionMode]composer.AppMode{
		ModeNormal:  composer.AppNormal,
		ModeInsert:  composer.AppInsert,
		ModeVi:      composer.AppVi,
		ModeCommand: composer.AppCommand,
	}
	for mode, expect := range want {
		if got := composerMode(mode); got != expect {
			t.Errorf("composerMode(%v) = %v, want %v", mode, got, expect)
		}
	}

	// Every declared mode is covered. A new one added to mode.go without a
	// case above fails here rather than at a user.
	for mode := ModeNormal; mode <= ModeCommand; mode++ {
		if _, ok := want[mode]; !ok {
			t.Errorf("InteractionMode %v has no expectation here", mode)
		}
	}
	if _, ok := want[ModeCommand+1]; ok {
		t.Fatal("this loop assumes ModeCommand is the last mode")
	}
}

// TestComposerBadgeAgreesWithTheResolver. Decision 3: the badge describes key
// routing rather than altering it, which is only true if it says what the
// resolver says. Two surfaces reading one derivation is what makes them
// unable to disagree.
func TestComposerBadgeAgreesWithTheResolver(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Model)
		want  string
	}{
		{"chat list focus", func(m *Model) { m.setFocus(PanelChatList) }, "NORMAL"},
		{"chat view focus", func(m *Model) { m.setFocus(PanelChatView) }, "NORMAL"},
		{"composer focus", func(m *Model) { m.setFocus(PanelComposer) }, "INSERT"},
		{"palette open", func(m *Model) {
			m.setFocus(PanelChatList)
			m.palette.Open()
		}, "COMMAND"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedMainModel(t)
			tc.setup(&m)
			m.refreshChrome()

			if got := m.Mode().String(); got != tc.want {
				t.Fatalf("resolver says %s, want %s", got, tc.want)
			}
			row := ansi.Strip(m.composer.View())
			if !strings.Contains(row, tc.want) {
				t.Errorf("composer badge does not say %s:\n%s", tc.want, row)
			}
		})
	}
}

// TestDraftSurvivesAChatSwitch is decision 13 end to end: the composer parks
// it, the chat list is told, and the layout is recomputed because a restored
// reply bar changes how many rows the composer takes.
func TestDraftSurvivesAChatSwitch(t *testing.T) {
	m := sizedMainModel(t)
	for _, id := range []int64{42, 43} {
		m.store.Chats.Set(&telegram.Chat{ID: id, Type: telegram.ChatTypePrivate, Title: "chat", Order: id})
	}

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: 42})
	m.setFocus(PanelComposer)
	typeInto(t, &m, "half a thought")

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: 43})
	if m.composer.HasDraft() {
		t.Fatalf("the new chat inherited a draft")
	}
	if !m.composer.HasDraftFor(42) {
		t.Fatalf("chat 42's draft was discarded rather than parked")
	}

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: 42})
	if !m.composer.HasDraft() {
		t.Fatalf("the draft did not come back")
	}
	if m.composer.HasDraftFor(42) {
		t.Errorf("the open chat still reports a parked draft")
	}
}

// TestChatListLearnsAboutParkedDrafts. The preview row is the only place a
// draft in another chat is visible at all; without it, unsent work is
// invisible the moment you look away from it.
//
// This checks the projection the app is responsible for. What the list draws
// from it is chatlist.TestDraftPreviewOutranksTheLastMessage — the list's own
// loading state is not reachable from here, and splitting it that way leaves
// each package testing the half it owns.
func TestChatListLearnsAboutParkedDrafts(t *testing.T) {
	m := sizedMainModel(t)
	for _, id := range []int64{42, 43} {
		m.store.Chats.Set(&telegram.Chat{ID: id, Type: telegram.ChatTypePrivate, Title: "chat", Order: id})
	}

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: 42})
	m.setFocus(PanelComposer)
	typeInto(t, &m, "unsent")
	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: 43})

	drafts := m.composer.DraftChats()
	if !drafts[42] {
		t.Errorf("chat 42 holds a parked draft and is not in %v", drafts)
	}
	if drafts[43] {
		t.Errorf("the open chat is in %v", drafts)
	}
}

// TestComposerRowsComeOutOfTheThread. The composer's rows are taken from the
// thread's budget, so the two must agree about how many there are or the
// history is drawn under the composer.
func TestComposerRowsComeOutOfTheThread(t *testing.T) {
	m := sizedMainModel(t)
	before := m.layout.ThreadHeight

	m.composer.EnterReplyMode(7, "nadia: rebased")
	m.updateLayout()

	if m.layout.ComposerHeight != 2 {
		t.Fatalf("ComposerHeight = %d with a reply bar, want 2", m.layout.ComposerHeight)
	}
	if m.layout.ThreadHeight != before-1 {
		t.Errorf("ThreadHeight = %d, want %d — the reply bar's row came from somewhere else",
			m.layout.ThreadHeight, before-1)
	}
}

// typeInto puts text into the composer the way a user would, one decoded
// keypress at a time.
func typeInto(t *testing.T, m *Model, text string) {
	t.Helper()
	for _, r := range text {
		*m = send(t, *m, decodeKey(t, string(r)))
	}
	if !m.composer.HasDraft() {
		t.Fatalf("typing %q left no draft", text)
	}
}
