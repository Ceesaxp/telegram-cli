package composer

import (
	"slices"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// user is a member fixture: an ID, a name and, when the second argument is
// not empty, a username.
func user(id int64, username, first, last string) *telegram.User {
	return &telegram.User{ID: id, Username: username, FirstName: first, LastName: last}
}

func ids(users []*telegram.User) []int64 {
	out := make([]int64, len(users))
	for i, u := range users {
		out[i] = u.ID
	}
	return out
}

// TestRankMentions is the ranking in one table (issue #41). Each case puts
// the better match LATER in the local list, so the order it comes out in is
// the tier's doing and not the recency's.
func TestRankMentions(t *testing.T) {
	cases := []struct {
		name          string
		query         string
		local, server []*telegram.User
		want          []int64
	}{
		{
			name:  "an exact username beats a username prefix",
			query: "nadia",
			local: []*telegram.User{user(2, "nadias", "Nadia", "S."), user(1, "nadia", "Nadia", "Petrova")},
			want:  []int64{1, 2},
		},
		{
			name:  "the exact match ignores case",
			query: "NADIA",
			local: []*telegram.User{user(2, "nadias", "", ""), user(1, "Nadia", "", "")},
			want:  []int64{1, 2},
		},
		{
			name:  "a username prefix beats a name word prefix",
			query: "pe",
			local: []*telegram.User{user(1, "", "Nadia", "Petrova"), user(2, "pete", "Peter", "")},
			want:  []int64{2, 1},
		},
		{
			name:  "any word of the name can be the prefix",
			query: "pet",
			local: []*telegram.User{user(1, "", "Nadia", "Petrova")},
			want:  []int64{1},
		},
		{
			name:  "a name word prefix beats a substring",
			query: "ova",
			local: []*telegram.User{user(1, "", "Nadia", "Petrova"), user(2, "", "Ovanes", "")},
			want:  []int64{2, 1},
		},
		{
			name:  "a substring beats a subsequence",
			query: "dia",
			local: []*telegram.User{user(1, "", "Dmitri", "Adamov"), user(2, "", "Nadia", "")},
			want:  []int64{2, 1},
		},
		{
			name:  "a username substring counts",
			query: "port",
			local: []*telegram.User{user(1, "nadia_support", "Help", "Desk")},
			want:  []int64{1},
		},
		{
			name:  "a user who matches nowhere is left out",
			query: "dia",
			local: []*telegram.User{user(1, "oleg", "Oleg", "")},
			want:  []int64{},
		},
		{
			name:  "within a tier, the more recent member comes first",
			query: "nad",
			local: []*telegram.User{user(9, "nadz", "Zed", ""), user(1, "nada", "Anna", "")},
			want:  []int64{9, 1},
		},
		{
			name:   "a member only the server knows comes after the known ones",
			query:  "nad",
			local:  []*telegram.User{user(9, "nadz", "Zed", "")},
			server: []*telegram.User{user(1, "nada", "Anna", "")},
			want:   []int64{9, 1},
		},
		{
			name:   "without recency, the name decides",
			query:  "nad",
			server: []*telegram.User{user(1, "nadb", "Bob", ""), user(2, "nada", "alice", "")},
			want:   []int64{2, 1},
		},
		{
			name:   "and without a name to tell them apart, the ID",
			query:  "nad",
			server: []*telegram.User{user(5, "nadb", "Sam", ""), user(3, "nada", "Sam", "")},
			want:   []int64{3, 5},
		},
		{
			name:   "a user in both lists appears once, at the local place",
			query:  "",
			local:  []*telegram.User{user(1, "", "Nadia", ""), user(2, "", "Oleg", "")},
			server: []*telegram.User{user(3, "", "Anna", ""), user(2, "oleg", "Oleg", "")},
			want:   []int64{1, 2, 3},
		},
		{
			name:  "an empty query is the local list in recency order",
			query: "",
			local: []*telegram.User{user(3, "", "Zed", ""), user(1, "", "Anna", ""), user(2, "", "Bob", "")},
			want:  []int64{3, 1, 2},
		},
		{
			name:  "nobody to call a mention by is no candidate",
			query: "",
			local: []*telegram.User{nil, user(0, "ghost", "Ghost", ""), user(4, "", "", ""), user(5, "", "Eve", "")},
			want:  []int64{5},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(rankMentions(tc.query, tc.local, tc.server))
			if !slices.Equal(got, tc.want) {
				t.Errorf("rankMentions(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// A user in both lists is shown as the server has them: the member search is
// the fresher of the two, and a local copy built from a message sender can be
// missing the username.
func TestRankMentionsPrefersTheServersCopy(t *testing.T) {
	got := rankMentions("",
		[]*telegram.User{user(2, "", "Oleg", "")},
		[]*telegram.User{user(2, "oleg", "Oleg", "")})
	if len(got) != 1 || got[0].Username != "oleg" {
		t.Fatalf("rankMentions = %+v, want the server's copy with its username", got)
	}
}
