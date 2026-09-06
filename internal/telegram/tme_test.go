package telegram

import "testing"

// zeroChannel is TDLib's base for channel chat IDs, written out here rather
// than taken from the constant the parser uses.
//
// The test states the arithmetic independently on purpose: asserting
// -1000000000000-2233445566 against a number computed by the same helper
// under test would only prove the helper agrees with itself, and the whole
// risk in a t.me/c/ link is getting this one conversion wrong.
const zeroChannel = -1000000000000

// maxChannel is gotd's constant.MaxTDLibChannelID, written out for the same
// reason zeroChannel is: the bound is the whole point of the test, and a
// bound asserted against the constant the parser reads would hold whatever
// that constant became.
const maxChannel = 997852516352

// maxMessage is the largest int32, which is what a message ID is on the
// wire.
const maxMessage = 2147483647

// TestParseTmeLink is the parser's whole contract: which links this client
// follows itself, which it hands to the browser, and which it refuses
// because they are not what they look like.
func TestParseTmeLink(t *testing.T) {
	for _, tc := range []struct {
		name string
		uri  string
		want TmeLink
		ok   bool
	}{
		// The three forms that navigate.
		{"username", "https://t.me/telegram", TmeLink{Username: "telegram"}, true},
		{"username at message", "https://t.me/telegram/123",
			TmeLink{Username: "telegram", MessageID: 123}, true},
		{"private channel at message", "https://t.me/c/2233445566/789",
			TmeLink{ChatID: zeroChannel - 2233445566, MessageID: 789}, true},
		{"private channel alone", "https://t.me/c/2233445566",
			TmeLink{ChatID: zeroChannel - 2233445566}, true},

		// Spellings that mean the same thing.
		{"no scheme", "t.me/telegram", TmeLink{Username: "telegram"}, true},
		{"http", "http://t.me/telegram", TmeLink{Username: "telegram"}, true},
		{"uppercase host", "https://T.ME/telegram", TmeLink{Username: "telegram"}, true},
		{"trailing slash", "https://t.me/telegram/", TmeLink{Username: "telegram"}, true},
		{"trailing slash after message", "https://t.me/telegram/123/",
			TmeLink{Username: "telegram", MessageID: 123}, true},
		// Usernames are case-insensitive, and the resolver takes only the
		// lower-case spelling, so /Telegram and /telegram must not be two
		// destinations.
		{"mixed-case username", "https://t.me/TeleGram", TmeLink{Username: "telegram"}, true},
		{"underscored username", "https://t.me/some_chat_1",
			TmeLink{Username: "some_chat_1"}, true},

		// Not navigation: following these would DO something.
		{"invite hash", "https://t.me/+AbCdEf123", TmeLink{}, false},
		{"joinchat invite", "https://t.me/joinchat/AbCdEf123", TmeLink{}, false},
		{"proxy", "https://t.me/proxy/server", TmeLink{}, false},
		{"socks", "https://t.me/socks/server", TmeLink{}, false},
		{"share", "https://t.me/share/url", TmeLink{}, false},
		{"web preview", "https://t.me/s/telegram", TmeLink{}, false},
		{"stickers", "https://t.me/addstickers/pack", TmeLink{}, false},
		{"boost", "https://t.me/boost/telegram", TmeLink{}, false},

		// Not intercepted, each for its own reason.
		{"tg scheme", "tg://resolve?domain=telegram", TmeLink{}, false},
		{"bot deep link", "https://t.me/somebot?start=payload", TmeLink{}, false},
		{"comment link", "https://t.me/telegram/123?comment=9", TmeLink{}, false},
		{"single from an album", "https://t.me/telegram/123?single", TmeLink{}, false},
		{"fragment", "https://t.me/telegram#top", TmeLink{}, false},
		{"forum topic", "https://t.me/telegram/12/345", TmeLink{}, false},
		{"private forum topic", "https://t.me/c/2233445566/12/345", TmeLink{}, false},
		{"bare host", "https://t.me", TmeLink{}, false},
		{"bare host with slash", "https://t.me/", TmeLink{}, false},
		{"c with no id", "https://t.me/c", TmeLink{}, false},
		{"mailto that mentions the host", "mailto:someone@t.me", TmeLink{}, false},

		// Hostile shapes. Each of these reads as a t.me link at a glance
		// and resolves somewhere else.
		{"lookalike suffix host", "https://t.me.evil.com/telegram", TmeLink{}, false},
		{"lookalike prefix host", "https://evil-t.me/telegram", TmeLink{}, false},
		{"host as a path", "https://evil.com/t.me/telegram", TmeLink{}, false},
		{"credentials in the authority", "https://t.me@evil.com/telegram", TmeLink{}, false},
		// A port makes t.me:8080 a different origin from the t.me the
		// reader was shown, and Hostname() drops it — including the
		// default port spelled out, because Telegram never writes one and
		// this parser matches what Telegram writes.
		{"nonstandard port", "https://t.me:8080/alice", TmeLink{}, false},
		{"default port spelled out", "https://t.me:443/alice", TmeLink{}, false},
		{"at-prefixed username", "https://t.me/@telegram", TmeLink{}, false},
		{"encoded separator", "https://t.me/telegram%2F123", TmeLink{}, false},
		{"encoded username", "https://t.me/%74elegram", TmeLink{}, false},
		{"negative message id", "https://t.me/telegram/-1", TmeLink{}, false},
		{"signed message id", "https://t.me/telegram/+12", TmeLink{}, false},
		{"non-numeric message id", "https://t.me/telegram/abc", TmeLink{}, false},
		{"zero message id", "https://t.me/telegram/0", TmeLink{}, false},
		{"overlong message id", "https://t.me/telegram/99999999999999999999", TmeLink{}, false},

		// Numbers that fit in an int64 and in nothing else. A channel ID
		// past the maximum wraps through channelChatID's
		// -1000000000000 - id into a POSITIVE chat ID naming an unrelated
		// peer, and a message ID past int32 is truncated by the cast the
		// client makes to build the request: both are links to nothing,
		// and the parser has to say so rather than hand on the wrong
		// destination.
		{"channel id that overflows the conversion",
			"https://t.me/c/9223372036854775807/1", TmeLink{}, false},
		{"largest channel id", "https://t.me/c/997852516352",
			TmeLink{ChatID: zeroChannel - maxChannel}, true},
		{"channel id past the maximum", "https://t.me/c/997852516353", TmeLink{}, false},
		{"largest message id", "https://t.me/telegram/2147483647",
			TmeLink{Username: "telegram", MessageID: maxMessage}, true},
		{"message id past int32", "https://t.me/telegram/2147483648", TmeLink{}, false},
		{"private channel message id past int32", "https://t.me/c/2233445566/2147483648",
			TmeLink{}, false},
		{"username starting with a digit", "https://t.me/1telegram", TmeLink{}, false},
		{"username ending in underscore", "https://t.me/telegram_", TmeLink{}, false},
		{"overlong username", "https://t.me/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TmeLink{}, false},
		{"empty", "", TmeLink{}, false},
		{"not a url at all", "://", TmeLink{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseTmeLink(tc.uri)
			if ok != tc.ok {
				t.Fatalf("ParseTmeLink(%q) ok = %v, want %v (got %+v)",
					tc.uri, ok, tc.ok, got)
			}
			if got != tc.want {
				t.Errorf("ParseTmeLink(%q) = %+v, want %+v", tc.uri, got, tc.want)
			}
		})
	}
}

// TestParseTmeLinkChannelIDRoundTrips pins the one conversion a private
// channel link depends on: the number in the path is the BARE channel ID,
// and the ID this codebase stores is the -100-prefixed TDLib form. Getting
// it wrong opens a different chat, or none.
func TestParseTmeLinkChannelIDRoundTrips(t *testing.T) {
	const channelID = 1234567890
	link, ok := ParseTmeLink("https://t.me/c/1234567890/5")
	if !ok {
		t.Fatal("a private channel link did not parse")
	}
	if got := plainChatID(link.ChatID); got != channelID {
		t.Errorf("chat ID %d converts back to %d, want %d",
			link.ChatID, got, channelID)
	}
	if link.ChatID != channelChatID(channelID) {
		t.Errorf("chat ID %d is not the canonical form %d",
			link.ChatID, channelChatID(channelID))
	}
}
