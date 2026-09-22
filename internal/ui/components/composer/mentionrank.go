package composer

import (
	"cmp"
	"slices"
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// How well a member matches what follows the @, best first (issue #41).
//
// Substring and subsequence are two tiers rather than one. The issue groups
// them as "substring/fuzzy", but "dia" inside "Nadia" is a far better answer
// than d…i…a scattered through "Dmitri Adamov", and a tie between them would
// be settled by nothing better than who spoke last.
const (
	matchExactUsername = iota
	matchUsernamePrefix
	matchWordPrefix
	matchSubstring
	matchSubsequence
)

// candidate is one member the picker can offer, and where it stands.
//
// recency is the member's place in the local list, most recent first. A
// member only the server returned has no place of its own there, so every
// one of them shares the place after the last local member and falls through
// to the name.
type candidate struct {
	user    *telegram.User
	recency int
	tier    int
}

// rankMentions orders the members who match query, best first, for the
// picker to show.
//
// local are the members the host already knows — recent senders, a basic
// group's member list — most recent first. server are what the member search
// returned for this query. The two are merged by user ID, keeping the local
// place and the server's copy of the user: the search is the fresher of the
// two, and a user built from a message sender can be missing the username.
//
// The order is total, so the same members always come out the same way:
//
//  1. exact username, ignoring case;
//  2. username prefix;
//  3. a prefix of any word of the display name;
//  4. a substring of the username or the name;
//  5. the query's letters, in order, anywhere in either (a subsequence);
//
// then the more recent member, then the name, then the ID. An empty query
// matches everybody at once, which leaves the local list in recency order.
// A member matching nowhere is left out.
//
// A user with neither a name nor a username is left out too: there is
// nothing to insert that would say who they are.
func rankMentions(query string, local, server []*telegram.User) []*telegram.User {
	q := strings.ToLower(query)

	var all []candidate
	at := map[int64]int{}
	for i, u := range local {
		if !mentionable(u) {
			continue
		}
		// A member listed twice keeps the first, most recent, place.
		if _, seen := at[u.ID]; seen {
			continue
		}
		at[u.ID] = len(all)
		all = append(all, candidate{user: u, recency: i})
	}
	for _, u := range server {
		if !mentionable(u) {
			continue
		}
		if i, seen := at[u.ID]; seen {
			// The server's copy replaces the local one; the place stays.
			all[i].user = u
			continue
		}
		at[u.ID] = len(all)
		all = append(all, candidate{user: u, recency: len(local)})
	}

	matched := all[:0]
	for _, c := range all {
		tier, ok := matchTier(c.user, q)
		if !ok {
			continue
		}
		c.tier = tier
		matched = append(matched, c)
	}

	slices.SortFunc(matched, func(a, b candidate) int {
		return cmp.Or(
			cmp.Compare(a.tier, b.tier),
			cmp.Compare(a.recency, b.recency),
			cmp.Compare(strings.ToLower(displayName(a.user)), strings.ToLower(displayName(b.user))),
			cmp.Compare(a.user.ID, b.user.ID),
		)
	})

	out := make([]*telegram.User, len(matched))
	for i, c := range matched {
		out[i] = c.user
	}
	return out
}

// matchTier says how well u matches q, which is already lower case, and
// whether it matches at all.
func matchTier(u *telegram.User, q string) (int, bool) {
	if q == "" {
		return matchExactUsername, true
	}
	username := strings.ToLower(u.Username)
	name := strings.ToLower(displayName(u))
	switch {
	case username == q:
		return matchExactUsername, true
	case username != "" && strings.HasPrefix(username, q):
		return matchUsernamePrefix, true
	case slices.ContainsFunc(strings.Fields(name), func(w string) bool { return strings.HasPrefix(w, q) }):
		return matchWordPrefix, true
	case strings.Contains(username, q) || strings.Contains(name, q):
		return matchSubstring, true
	case subsequence(username, q) || subsequence(name, q):
		return matchSubsequence, true
	}
	return 0, false
}

// subsequence reports whether every rune of pattern appears in s, in order.
// It is the "fuzzy" of the ranking: "ndpt" finds "nadia petrova".
//
// The command palette has its own copy. Two components sharing one would
// mean a package for a dozen lines, and they need not stay alike.
func subsequence(s, pattern string) bool {
	p := []rune(pattern)
	if len(p) == 0 {
		return true
	}
	i := 0
	for _, r := range s {
		if r == p[i] {
			i++
			if i == len(p) {
				return true
			}
		}
	}
	return false
}

// displayName is what a member is called: "First Last", or whichever of the
// two they have.
func displayName(u *telegram.User) string {
	return strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName))
}

// mentionable reports whether u is somebody the picker can offer: a real
// user, with a username or a name to insert.
func mentionable(u *telegram.User) bool {
	return u != nil && u.ID != 0 && (u.Username != "" || displayName(u) != "")
}
