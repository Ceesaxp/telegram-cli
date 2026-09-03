package store

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// TestMemberCountSurvivesADialog is the whole reason it is a field of its
// own. A dialog says who a chat is, not how many people are in it, so the
// update that carries one must not take the other away — the same rule Set
// and Merge were separated for.
func TestMemberCountSurvivesADialog(t *testing.T) {
	s := NewChatStore()
	s.Set(&telegram.Chat{ID: 7, Title: "infra-oncall", Type: telegram.ChatTypeSupergroup})
	s.SetMemberCount(7, 24)

	s.Set(&telegram.Chat{ID: 7, Title: "infra-oncall", Type: telegram.ChatTypeSupergroup, UnreadCount: 4})
	if got := s.MemberCount(7); got != 24 {
		t.Errorf("after a dialog, member count = %d, want 24", got)
	}

	s.Merge(&telegram.Chat{ID: 7, Title: "infra oncall", Type: telegram.ChatTypeSupergroup})
	if got := s.MemberCount(7); got != 24 {
		t.Errorf("after a peer update, member count = %d, want 24", got)
	}
}

// TestAFailedMemberCountDoesNotErase. Every caller gets the number from a
// full-info call, and a call that failed hands back the zero value; storing
// it would take a number this client knows and replace it with one it does
// not.
func TestAFailedMemberCountDoesNotErase(t *testing.T) {
	s := NewChatStore()
	s.Set(&telegram.Chat{ID: 7, Type: telegram.ChatTypeSupergroup})
	s.SetMemberCount(7, 24)
	s.SetMemberCount(7, 0)

	if got := s.MemberCount(7); got != 24 {
		t.Errorf("member count = %d, want 24 — a failed call said nothing", got)
	}
}

// TestMemberCountForAChatNobodyHasDescribed. The count can arrive before
// the dialog does, and dropping it would make the header wait for a second
// call that never comes.
func TestMemberCountForAChatNobodyHasDescribed(t *testing.T) {
	s := NewChatStore()
	s.SetMemberCount(7, 24)

	if got := s.MemberCount(7); got != 24 {
		t.Fatalf("member count = %d, want 24", got)
	}
	entry, ok := s.Get(7)
	if !ok || !entry.Unresolved {
		t.Fatalf("entry = %+v, want an unresolved placeholder", entry)
	}

	// And the dialog, when it lands, names it without disturbing the count.
	s.Set(&telegram.Chat{ID: 7, Title: "infra-oncall", Type: telegram.ChatTypeSupergroup})
	if entry, _ := s.Get(7); entry.Unresolved || entry.MemberCount != 24 {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestMemberCountOfAnUnknownChatIsZero(t *testing.T) {
	if got := NewChatStore().MemberCount(7); got != 0 {
		t.Errorf("member count = %d, want 0", got)
	}
}

// TestTotalUnreadSumsEveryChat, which is what the hint bar reports and what
// a running counter would eventually get wrong.
func TestTotalUnreadSumsEveryChat(t *testing.T) {
	s := NewChatStore()
	if got := s.TotalUnread(); got != 0 {
		t.Errorf("an empty store has %d unread", got)
	}

	for id, unread := range map[int64]int32{1: 4, 2: 0, 3: 2, 4: 31} {
		s.Set(&telegram.Chat{ID: id, UnreadCount: unread})
	}
	if got := s.TotalUnread(); got != 37 {
		t.Errorf("total unread = %d, want 37", got)
	}

	// A chat that has been read stops counting.
	s.UpdateReadInbox(4, 0)
	if got := s.TotalUnread(); got != 6 {
		t.Errorf("after reading the loud one, total unread = %d, want 6", got)
	}
}
