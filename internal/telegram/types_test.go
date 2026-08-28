package telegram

import (
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gotd/td/tg"
)

const repl = "�"

func TestSanitizeTerminal(t *testing.T) {
	tests := map[string]string{
		"plain text":                   "plain text",
		"keeps\nnewlines\tand\ttabs":   "keeps\nnewlines\tand\ttabs",
		"unicode ok: héllo \U0001F600": "unicode ok: héllo \U0001F600",

		// Window retitle via OSC 0.
		"\x1b]0;pwned\x07": repl + "]0;pwned" + repl,
		// Clipboard write via OSC 52.
		"\x1b]52;c;cGF5bG9hZA==\x07": repl + "]52;c;cGF5bG9hZA==" + repl,

		"bell\x07and\rcr": "bell" + repl + "and" + repl + "cr",
		"del\x7fhere":     "del" + repl + "here",
		// C1 NEL and C1 CSI.
		"c1\u0085next": "c1" + repl + "next",
		"c1\u009bcsi":  "c1" + repl + "csi",
	}
	for input, want := range tests {
		if got := sanitizeTerminal(input); got != want {
			t.Errorf("sanitizeTerminal(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestSanitizeTerminalPreservesOffsets guards the invariant that
// FormattedText entity offsets depend on: sanitizing replaces, never
// deletes, so both the rune count and the UTF-16 code unit count are
// unchanged.
func TestSanitizeTerminalPreservesOffsets(t *testing.T) {
	inputs := []string{
		"\x1b]0;title\x07 bold text",
		"\x00\x01\x02\x1b\x7f",
		"mixed \U0001F600 \x1b[31m red \u009b end",
		"plain",
	}
	for _, in := range inputs {
		out := sanitizeTerminal(in)
		if got, want := utf8.RuneCountInString(out), utf8.RuneCountInString(in); got != want {
			t.Errorf("rune count changed for %q: got %d, want %d", in, got, want)
		}
		if got, want := len(utf16.Encode([]rune(out))), len(utf16.Encode([]rune(in))); got != want {
			t.Errorf("utf-16 length changed for %q: got %d, want %d", in, got, want)
		}
	}
}

func TestMutedFromNotifySettings(t *testing.T) {
	const now = 1000

	var silent tg.PeerNotifySettings
	silent.SetSilent(true)

	var notSilent tg.PeerNotifySettings
	notSilent.SetSilent(false)

	var muteFuture tg.PeerNotifySettings
	muteFuture.SetMuteUntil(now + 60)

	var mutePast tg.PeerNotifySettings
	mutePast.SetMuteUntil(now - 60)

	tests := []struct {
		name     string
		settings tg.PeerNotifySettings
		want     bool
	}{
		{"unset", tg.PeerNotifySettings{}, false},
		{"silent", silent, true},
		{"explicitly not silent", notSilent, false},
		{"mute until future", muteFuture, true},
		{"mute until past", mutePast, false},
	}
	for _, tt := range tests {
		if got := mutedFromNotifySettings(tt.settings, now); got != tt.want {
			t.Errorf("%s: mutedFromNotifySettings = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestChatIDFromInputPeer(t *testing.T) {
	tests := []struct {
		name   string
		peer   tg.InputPeerClass
		want   int64
		wantOK bool
	}{
		{"user", &tg.InputPeerUser{UserID: 42, AccessHash: 7}, userChatID(42), true},
		{"chat", &tg.InputPeerChat{ChatID: 42}, basicGroupChatID(42), true},
		{"channel", &tg.InputPeerChannel{ChannelID: 42, AccessHash: 7}, channelChatID(42), true},
		{"self", &tg.InputPeerSelf{}, 0, false},
		{"empty", &tg.InputPeerEmpty{}, 0, false},
		{"from message", &tg.InputPeerUserFromMessage{UserID: 42}, 0, false},
	}
	for _, tt := range tests {
		got, ok := chatIDFromInputPeer(tt.peer)
		if ok != tt.wantOK {
			t.Errorf("%s: ok = %v, want %v", tt.name, ok, tt.wantOK)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: chatIDFromInputPeer = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// TestFormattedTextFromTGConvertsUTF16Offsets pins the boundary
// conversion: Telegram sends UTF-16 code unit offsets, consumers index
// []rune, and a non-BMP character makes the two disagree.
func TestFormattedTextFromTGConvertsUTF16Offsets(t *testing.T) {
	// One emoji (1 rune, 2 UTF-16 units), a space, then "bold".
	const text = "\U0001F600 bold"

	// This is what Telegram sends for the "bold" span: UTF-16 offset 3,
	// length 4. Guard the premise of the test — with 6 runes, slicing
	// runes[3:7] with those raw values is out of range and panics.
	const rawOffset, rawLength = 3, 4
	if got := utf8.RuneCountInString(text); got != 6 {
		t.Fatalf("test text has %d runes, want 6", got)
	}
	if rawOffset+rawLength <= utf8.RuneCountInString(text) {
		t.Fatal("test text no longer demonstrates the bug: raw offsets are in range")
	}

	ft := formattedTextFromTG(text, []tg.MessageEntityClass{
		&tg.MessageEntityBold{Offset: rawOffset, Length: rawLength},
	})
	if len(ft.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(ft.Entities))
	}

	e := ft.Entities[0]
	if e.Offset != 2 || e.Length != 4 {
		t.Errorf("converted entity = offset %d length %d, want offset 2 length 4", e.Offset, e.Length)
	}

	runes := []rune(ft.Text)
	if int(e.Offset+e.Length) > len(runes) {
		t.Fatalf("converted entity still out of range: %d > %d", e.Offset+e.Length, len(runes))
	}
	if got := string(runes[e.Offset : e.Offset+e.Length]); got != "bold" {
		t.Errorf("entity spans %q, want %q", got, "bold")
	}
	if _, ok := e.Type.(*TextEntityTypeBold); !ok {
		t.Errorf("entity type = %T, want *TextEntityTypeBold", e.Type)
	}
}

// TestFormattedTextFromTGOffsetsSurviveSanitizing checks that stripping
// terminal control sequences does not shift entity offsets.
func TestFormattedTextFromTGOffsetsSurviveSanitizing(t *testing.T) {
	// ESC then "bold": the ESC is one UTF-16 unit and one rune both
	// before and after sanitizing, so the span must not move.
	const text = "\x1b \U0001F600 bold"

	ft := formattedTextFromTG(text, []tg.MessageEntityClass{
		&tg.MessageEntityBold{Offset: 5, Length: 4},
	})
	runes := []rune(ft.Text)
	e := ft.Entities[0]
	if got := string(runes[e.Offset : e.Offset+e.Length]); got != "bold" {
		t.Errorf("entity spans %q, want %q", got, "bold")
	}
	if got := string(runes[0]); got != repl {
		t.Errorf("ESC was not sanitized: %q", got)
	}
}

// TestFormattedTextFromTGClampsBadOffsets checks that hostile or stale
// offsets are clamped rather than propagated to a slicing consumer.
func TestFormattedTextFromTGClampsBadOffsets(t *testing.T) {
	const text = "short"

	tests := []struct {
		name           string
		offset, length int
	}{
		{"past the end", 100, 4},
		{"length overruns", 2, 1000},
		{"negative offset", -5, 3},
		{"negative length", 3, -10},
		{"max int length", 0, int(^uint(0) >> 1)},
	}
	for _, tt := range tests {
		ft := formattedTextFromTG(text, []tg.MessageEntityClass{
			&tg.MessageEntityBold{Offset: tt.offset, Length: tt.length},
		})
		runes := []rune(ft.Text)
		e := ft.Entities[0]
		if e.Offset < 0 || e.Length < 0 || int(e.Offset+e.Length) > len(runes) {
			t.Errorf("%s: entity offset %d length %d out of range for %d runes",
				tt.name, e.Offset, e.Length, len(runes))
			continue
		}
		// Must not panic.
		_ = string(runes[e.Offset : e.Offset+e.Length])
	}
}

func TestUTF16RuneIndex(t *testing.T) {
	// "a😀b": a=1 unit, emoji=2 units, b=1 unit -> 4 units, 3 runes.
	got := utf16RuneIndex("a\U0001F600b")
	want := []int32{0, 1, 1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("table length %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("table[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	if got := utf16RuneIndex(""); len(got) != 1 || got[0] != 0 {
		t.Errorf("empty string table = %v, want [0]", got)
	}
}

// TestDeletedMsgFor pins the event shape DeleteMessages publishes: it
// must match what the update listener emits, because chatview routes on
// ChatId == 0 to decide between Delete and DeleteFromAll.
func TestDeletedMsgFor(t *testing.T) {
	ids := []int64{7, 8, 9}

	tests := []struct {
		name       string
		chatID     int64
		wantChatID int64
	}{
		// Channels/supergroups: the listener names the chat, so we do too.
		{"channel", channelChatID(1234), channelChatID(1234)},
		// Users and basic groups: the server update carries no peer, so
		// the listener sends ChatId 0 and the store deletes from all
		// chats. Naming the chat here would diverge from that.
		{"user", userChatID(1234), 0},
		{"basic group", basicGroupChatID(1234), 0},
	}
	for _, tt := range tests {
		got := deletedMsgFor(tt.chatID, ids)
		if got.ChatId != tt.wantChatID {
			t.Errorf("%s: ChatId = %d, want %d", tt.name, got.ChatId, tt.wantChatID)
		}
		if len(got.MessageIds) != len(ids) {
			t.Errorf("%s: got %d message IDs, want %d", tt.name, len(got.MessageIds), len(ids))
			continue
		}
		for i := range ids {
			if got.MessageIds[i] != ids[i] {
				t.Errorf("%s: MessageIds[%d] = %d, want %d", tt.name, i, got.MessageIds[i], ids[i])
			}
		}
	}
}

func TestInt64sToInts(t *testing.T) {
	if got := int64sToInts(nil); len(got) != 0 {
		t.Errorf("int64sToInts(nil) = %v, want empty", got)
	}
	got := int64sToInts([]int64{1, 2, 3})
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %d elements, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	// Round-trips with the listener's inverse.
	ids := []int64{10, 20, 30}
	if back := intsToInt64s(int64sToInts(ids)); len(back) != 3 ||
		back[0] != 10 || back[1] != 20 || back[2] != 30 {
		t.Errorf("round trip = %v, want %v", back, ids)
	}
}

// TestDeleteMessagesEmptyIsNoop checks the early return: with no IDs the
// method must not touch the network (c.api is nil on this zero client,
// so any RPC attempt would panic).
func TestDeleteMessagesEmptyIsNoop(t *testing.T) {
	var c Client
	if err := c.DeleteMessages(userChatID(1), nil, true); err != nil {
		t.Errorf("DeleteMessages with no IDs = %v, want nil", err)
	}
	if err := c.DeleteMessages(channelChatID(1), []int64{}, false); err != nil {
		t.Errorf("DeleteMessages with empty IDs = %v, want nil", err)
	}
}

// TestSearchChatMessagesEmptyQuery checks the guard that keeps an empty
// query from becoming a guaranteed SEARCH_QUERY_EMPTY round trip. The
// zero Client has a nil api, so reaching the RPC would panic.
func TestSearchChatMessagesEmptyQuery(t *testing.T) {
	var c Client

	got, err := c.SearchChatMessages(userChatID(1), "", 0, 20)
	if err == nil {
		t.Fatal("SearchChatMessages with an empty query returned no error")
	}
	if got != nil {
		t.Errorf("got %d messages, want nil", len(got))
	}
	if !strings.HasPrefix(err.Error(), "search chat messages:") {
		t.Errorf("error %q lacks the %q prefix", err, "search chat messages:")
	}
}
