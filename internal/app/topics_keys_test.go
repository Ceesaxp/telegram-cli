package app

import "testing"

// The folder keys are the chat list's, and a forum's topics are in no
// folder (docs/topics.md, "The interaction").
//
// The chat list already refuses its own left/right and 1-9 while drilled in,
// but [ and ] are dispatched at APP level (decision I-1) and were not
// guarded: ] inside a forum switched a tab nobody could see, and announced
// itself on the way back out, as a chat list on a folder the reader had
// never chosen.

func TestTheFolderKeysAreInertInsideAForum(t *testing.T) {
	m := forumModel(t, &fakeForums{topics: goSerbiaTopics()})
	m.chatList.SetFoldersForTest([]string{"All", "Work", "News"})
	m = enterForum(t, m)
	m.setFocus(PanelChatList)

	for _, k := range []string{"]", "["} {
		before := m.chatList.ActiveFolderIndex()
		m = update(t, m, k)
		if got := m.chatList.ActiveFolderIndex(); got != before {
			t.Errorf("%q moved the folder tab from %d to %d while drilled into a forum",
				k, before, got)
		}
	}
	if got := m.chatList.ForumChatID(); got != testForumID {
		t.Errorf("the folder keys left the list drilled into %d, want the forum %d",
			got, testForumID)
	}
}

// And outside one they still cycle, or the guard above bought its quiet by
// taking the keys away everywhere.
func TestTheFolderKeysStillCycleOutsideAForum(t *testing.T) {
	m := forumModel(t, &fakeForums{topics: goSerbiaTopics()})
	m.chatList.SetFoldersForTest([]string{"All", "Work", "News"})
	m.setFocus(PanelChatList)

	m = update(t, m, "]")
	if got := m.chatList.ActiveFolderIndex(); got != 1 {
		t.Errorf("] left the folder tab at %d, want 1", got)
	}

	m = update(t, m, "[")
	if got := m.chatList.ActiveFolderIndex(); got != 0 {
		t.Errorf("[ left the folder tab at %d, want 0", got)
	}
}

// Leaving the forum gives the keys back: the guard is about where the
// reader is, not about the session having once been in a forum.
func TestTheFolderKeysComeBackOnLeavingAForum(t *testing.T) {
	m := forumModel(t, &fakeForums{topics: goSerbiaTopics()})
	m.chatList.SetFoldersForTest([]string{"All", "Work", "News"})
	m = leaveForum(t, enterForum(t, m))
	m.setFocus(PanelChatList)

	m = update(t, m, "]")

	if got := m.chatList.ActiveFolderIndex(); got != 1 {
		t.Errorf("] left the folder tab at %d after leaving the forum, want 1", got)
	}
}
