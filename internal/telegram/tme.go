package telegram

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/deeplink"
)

// A t.me link that names somewhere inside Telegram is not a web page. Handing
// it to the platform opener sends the reader out to a browser, which shows
// them a "VIEW IN TELEGRAM" interstitial, which tries to hand it back to a
// Telegram client — and the client it finds is not this one. So the link is
// read here and followed in place.
//
// This file is the READING half only: it is pure, it makes no network call,
// and it decides nothing about navigation. Deriving the chat ID is why it
// lives in this package rather than beside the link cursor in chatview: a
// t.me/c/… link carries the bare channel ID and the canonical chat ID is
// -100·channelID, which is a spelling this package already owns (see
// [channelChatID] and constant.TDLibPeerID). A second copy of that
// arithmetic anywhere else is a second place for it to be wrong.

// tmeHost is the only host read as a Telegram link.
//
// Exact, case-folded equality — never a suffix test. "t.me.evil.com" ends
// with the string "t.me" and is somebody else's domain entirely, and a
// client that navigated INTO an account on the strength of a hostname it
// only checked the tail of would be following the attacker's link rather
// than the writer's.
const tmeHost = "t.me"

// TmeLink is a t.me link that means "go here", already broken into the
// parts a navigation needs.
//
// Exactly one of Username and ChatID is set: a public link names a
// username that still has to be resolved over the network, and a
// private-channel link (t.me/c/…) carries the ID outright and needs no
// resolution at all. MessageID is 0 for a link to the chat itself.
type TmeLink struct {
	// Username is the public @name, lower-cased and without the @.
	// Telegram usernames are case-insensitive and the resolver only
	// accepts the lower-case spelling, so a link written /Telegram and one
	// written /telegram must not be two different destinations.
	Username string
	// ChatID is the canonical TDLib-style chat ID, set only for a
	// t.me/c/<id>/… link.
	ChatID int64
	// MessageID is the message to open the chat AT, or 0 for the newest.
	MessageID int64
}

// tmeNotNavigation are first path segments that make a t.me link something
// other than "go to this chat", and which therefore keep going to the
// platform opener.
//
// The rule is not "this client cannot draw it" — it is that following the
// link would DO something rather than move somewhere. An invite link
// (/joinchat/… and its modern /+hash spelling, handled separately) joins a
// chat, and joining is a decision the reader makes deliberately, not a
// side effect of pressing enter on a link cursor. /proxy and /socks
// reconfigure the connection. /share composes a message somewhere else.
// The rest — stickers, themes, a language pack, a boost, a gift code, an
// invoice, a login confirmation — are all actions of the same kind.
//
// /s/ is the odd one out and is excluded for the opposite reason: it is
// Telegram's own PUBLIC WEB preview of a channel, so a browser really is
// where it belongs.
var tmeNotNavigation = map[string]bool{
	"joinchat": true,
	"proxy":    true, "socks": true,
	"share": true, "iv": true, "s": true,
	"addstickers": true, "addemoji": true, "addtheme": true,
	"setlanguage": true, "confirmphone": true, "login": true,
	"bg": true, "invoice": true, "contact": true,
	"boost": true, "giftcode": true,
}

// maxMessageID is the largest message ID a link may name.
//
// Message IDs are int32 on the wire, and this client casts them back down
// to build a request, so a number that does not fit is not a message
// anything could be asked for — it is a link to nothing. Bounding it here
// means the parser never hands on an ID whose meaning changes on the way
// to the server.
const maxMessageID = 1<<31 - 1

// ParseTmeLink reads a URI as a Telegram navigation link.
//
// It reports false for everything it is not certain about, and that is the
// safe direction: a link this refuses still opens, in the browser, exactly
// as it did before. A link it wrongly accepts would open the WRONG PLACE
// inside the reader's own account, silently, which is not a failure mode a
// link cursor is allowed to have.
//
// Refused on purpose, each because following it would not be navigation:
//
//   - invite links, /+hash and /joinchat/hash — following one JOINS;
//   - the action links in [tmeNotNavigation];
//   - tg:// deeplinks. The scheme carries a dozen verbs (resolve, join,
//     proxy, addstickers, settings) and picking off the one that navigates
//     would leave the rest going to an opener that, on a machine with no
//     Telegram client installed, has no handler for them either. Reading
//     the whole scheme is a bigger feature than this one;
//   - anything carrying a query string or a fragment. ?start= is a bot
//     deep link, ?comment= names a comment and ?single picks one photo out
//     of an album: each is a modifier this client does not implement, and
//     dropping it silently would go somewhere the link did not say.
//
// A third path segment is refused too. t.me/<chat>/<topic>/<id> is a forum
// topic, and while the message is reachable, this client has no topic
// surface to land the reader on — so the browser keeps it until it does.
func ParseTmeLink(raw string) (TmeLink, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TmeLink{}, false
	}
	// A bare "t.me/name" is defaulted to https the same way [entityURI]
	// defaults a bare URL entity, so the two agree about what a scheme-less
	// link in a message meant.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return TmeLink{}, false
	}
	// url.Parse lower-cases the scheme itself, so this needs no folding.
	if u.Scheme != "http" && u.Scheme != "https" {
		return TmeLink{}, false
	}
	// Credentials in the authority are the classic way to make a URL read
	// as one host and resolve as another. Telegram never writes one, so a
	// link that has one is not a link this client should be following.
	if u.User != nil {
		return TmeLink{}, false
	}
	// A port is the same substitution spelled differently: Hostname()
	// drops it, so "t.me:8080" passes a host check and is then followed as
	// a chat inside the reader's own account while the URI they were shown
	// named a different origin. Interception is only honest while the
	// authority it matched is the one the link names, so the match has to
	// be exact — and Telegram never writes a port, not even 443, so any
	// port at all means this was not written by Telegram.
	if u.Port() != "" {
		return TmeLink{}, false
	}
	if !strings.EqualFold(u.Hostname(), tmeHost) {
		return TmeLink{}, false
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return TmeLink{}, false
	}

	// The ESCAPED path, split without decoding: a "%2F" inside a segment
	// stays inside it rather than becoming a separator, so the number of
	// segments is what the link's author wrote. The percent sign then
	// fails the username and digit checks below on its own.
	var seg []string
	for _, s := range strings.Split(u.EscapedPath(), "/") {
		if s != "" {
			seg = append(seg, s)
		}
	}

	switch {
	case len(seg) == 0:
		// t.me itself is the download page, not a chat.
		return TmeLink{}, false

	case seg[0] == "c":
		// t.me/c/<channelID>[/<messageID>] — a private channel or
		// supergroup, which has no username to resolve because that is
		// precisely what makes it private. A bare t.me/c names nothing.
		if len(seg) < 2 || len(seg) > 3 {
			return TmeLink{}, false
		}
		id, ok := parsePositiveID(seg[1], constant.MaxTDLibChannelID)
		if !ok {
			return TmeLink{}, false
		}
		link := TmeLink{ChatID: channelChatID(id)}
		if len(seg) == 3 {
			if link.MessageID, ok = parsePositiveID(seg[2], maxMessageID); !ok {
				return TmeLink{}, false
			}
		}
		return link, true

	case len(seg) > 2, strings.HasPrefix(seg[0], "+"), tmeNotNavigation[seg[0]]:
		return TmeLink{}, false

	default:
		name := strings.ToLower(seg[0])
		if deeplink.ValidateDomain(name) != nil {
			return TmeLink{}, false
		}
		link := TmeLink{Username: name}
		if len(seg) == 2 {
			var ok bool
			if link.MessageID, ok = parsePositiveID(seg[1], maxMessageID); !ok {
				return TmeLink{}, false
			}
		}
		return link, true
	}
}

// parsePositiveID reads a path segment as a decimal ID in [1, max].
//
// The upper bound is taken here rather than left to the call sites because
// this parser's contract is to fail closed, and an out-of-range ID is the
// one way an ACCEPTED link still means nothing. A channel ID above
// constant.MaxTDLibChannelID is not merely large: [channelChatID] is
// ZeroTDLibChannelID minus it, so t.me/c/9223372036854775807 wraps back
// around into a POSITIVE chat ID naming some unrelated peer — reported,
// until the bound existed, with ok = true.
//
// The digit sweep is not redundant with ParseInt: ParseInt accepts a
// leading sign and an underscore separator, neither of which is a thing a
// t.me link contains — and "+1" reaching this as a channel ID would be an
// invite hash read as a number.
func parsePositiveID(s string, max int64) (int64, bool) {
	if s == "" || len(s) > 19 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 || id > max {
		return 0, false
	}
	return id, true
}
