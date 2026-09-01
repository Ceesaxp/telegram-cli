package telegram

import (
	"strings"
	"testing"

	"github.com/gotd/td/tg"
)

func TestReactionsCarryTheirTallies(t *testing.T) {
	chosen := tg.ReactionCount{Reaction: &tg.ReactionEmoji{Emoticon: "👍"}, Count: 3}
	chosen.SetChosenOrder(0)

	got := reactionsFromTG(tg.MessageReactions{Results: []tg.ReactionCount{
		chosen,
		{Reaction: &tg.ReactionEmoji{Emoticon: "🔥"}, Count: 2},
		{Reaction: &tg.ReactionCustomEmoji{DocumentID: 77}, Count: 5},
		{Reaction: &tg.ReactionEmpty{}, Count: 9},
	}})

	if len(got) != 3 {
		t.Fatalf("got %d reactions, want 3 (the empty one is not a reaction)", len(got))
	}
	if got[0].Emoji != "👍" || got[0].Count != 3 || !got[0].Chosen {
		t.Errorf("first = %+v", *got[0])
	}
	if got[1].Emoji != "🔥" || got[1].Count != 2 || got[1].Chosen {
		t.Errorf("second = %+v", *got[1])
	}
	if got[2].Emoji != "" || got[2].CustomEmojiID != 77 || got[2].Count != 5 {
		t.Errorf("custom = %+v", *got[2])
	}
}

// TestReactionsSanitizeTheirEmoji. A reaction's emoticon is a string the
// server hands back, and it reaches the terminal the same way message text
// does.
func TestReactionsSanitizeTheirEmoji(t *testing.T) {
	got := reactionsFromTG(tg.MessageReactions{Results: []tg.ReactionCount{
		{Reaction: &tg.ReactionEmoji{Emoticon: "\x1b]0;pwned\x07"}, Count: 1},
	}})
	if len(got) != 1 {
		t.Fatalf("got %d reactions, want 1", len(got))
	}
	if strings.ContainsRune(got[0].Emoji, 0x1b) {
		t.Fatalf("an escape survived: %q", got[0].Emoji)
	}
}

// TestNoReactionsIsNilNotEmpty. The renderer asks len(Reactions), and a
// zero-length non-nil slice would make a message with no reactions and a
// message whose reactions were all unrecognised look like different things
// to anything that compares against nil.
func TestNoReactionsIsNilNotEmpty(t *testing.T) {
	if got := reactionsFromTG(tg.MessageReactions{}); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
	unknown := tg.MessageReactions{Results: []tg.ReactionCount{
		{Reaction: &tg.ReactionEmpty{}, Count: 4},
	}}
	if got := reactionsFromTG(unknown); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

// answer is a poll answer with its opaque option bytes.
func answer(text, option string) tg.PollAnswerClass {
	return &tg.PollAnswer{Text: tg.TextWithEntities{Text: text}, Option: []byte(option)}
}

func TestPollCarriesItsAnswersAndTallies(t *testing.T) {
	p := tg.Poll{
		Question: tg.TextWithEntities{Text: "Ship 0.4.2 tonight?"},
		Answers:  []tg.PollAnswerClass{answer("Yes, tag it", "a"), answer("Wait for keymap", "b"), answer("Abstain", "c")},
	}
	p.SetCloseDate(1_700_000_000)

	// Deliberately out of order, and keyed by option bytes: the tallies are
	// not promised to arrive in the answers' order.
	results := tg.PollResults{Results: []tg.PollAnswerVoters{
		{Option: []byte("c"), Voters: 1},
		{Option: []byte("a"), Voters: 7, Chosen: true},
		{Option: []byte("b"), Voters: 3},
	}}
	results.SetTotalVoters(11)

	poll := pollFromTG(p, results)

	if poll.Question != "Ship 0.4.2 tonight?" {
		t.Fatalf("question = %q", poll.Question)
	}
	if !poll.ResultsKnown {
		t.Fatal("results arrived and the poll says they did not")
	}
	if poll.TotalVoterCount != 11 {
		t.Fatalf("total = %d, want 11", poll.TotalVoterCount)
	}
	if poll.CloseDate != 1_700_000_000 {
		t.Fatalf("close date = %d", poll.CloseDate)
	}
	if !poll.IsAnonymous {
		t.Fatal("a poll without public voters is anonymous")
	}

	want := []struct {
		text    string
		voters  int32
		percent int32
		chosen  bool
	}{
		{"Yes, tag it", 7, 64, true},
		{"Wait for keymap", 3, 27, false},
		{"Abstain", 1, 9, false},
	}
	if len(poll.Options) != len(want) {
		t.Fatalf("got %d options, want %d", len(poll.Options), len(want))
	}
	for i, w := range want {
		got := poll.Options[i]
		if got.Text != w.text || got.VoterCount != w.voters || got.Percent != w.percent || got.Chosen != w.chosen {
			t.Errorf("option %d = %+v, want %v", i, *got, w)
		}
	}
}

// TestPollWithoutResultsSaysSo. A poll that hides its results until it
// closes sends no tallies at all, and the difference between "no votes" and
// "not told" is the difference between a bar and no bar.
func TestPollWithoutResultsSaysSo(t *testing.T) {
	poll := pollFromTG(tg.Poll{
		Question: tg.TextWithEntities{Text: "Which?"},
		Answers:  []tg.PollAnswerClass{answer("A", "a"), answer("B", "b")},
	}, tg.PollResults{})

	if poll.ResultsKnown {
		t.Fatal("no tallies arrived and the poll claims to know the results")
	}
	if len(poll.Options) != 2 {
		t.Fatalf("got %d options, want 2", len(poll.Options))
	}
	for i, option := range poll.Options {
		if option.VoterCount != 0 || option.Percent != 0 {
			t.Errorf("option %d invented a result: %+v", i, *option)
		}
	}
	if poll.TotalVoterCount != 0 {
		t.Fatalf("total = %d, want 0 — the server sent none", poll.TotalVoterCount)
	}
}

func TestPollFlagsSurvive(t *testing.T) {
	poll := pollFromTG(tg.Poll{
		Question:       tg.TextWithEntities{Text: "?"},
		Closed:         true,
		PublicVoters:   true,
		MultipleChoice: true,
		Quiz:           true,
	}, tg.PollResults{})

	if !poll.IsClosed || !poll.MultipleChoice || !poll.IsQuiz {
		t.Fatalf("flags lost: %+v", *poll)
	}
	if poll.IsAnonymous {
		t.Fatal("a poll with public voters is not anonymous")
	}
	if poll.CloseDate != 0 {
		t.Fatalf("close date = %d, want 0 — the poll has no scheduled end", poll.CloseDate)
	}
}

// TestApportionSumsToOneHundred is the reason percentages are not computed
// per option. Three equal thirds round to 33 each and a reader adds them up.
func TestApportionSumsToOneHundred(t *testing.T) {
	cases := map[string]struct {
		counts []int32
		want   []int32
	}{
		"the design record's poll": {[]int32{7, 3, 1}, []int32{64, 27, 9}},
		"three equal thirds":       {[]int32{1, 1, 1}, []int32{34, 33, 33}},
		"a single option":          {[]int32{5}, []int32{100}},
		"one option unvoted":       {[]int32{1, 0}, []int32{100, 0}},
		"nobody voted":             {[]int32{0, 0}, []int32{0, 0}},
		"no options at all":        {nil, nil},
		"large counts":             {[]int32{1_000_001, 2}, []int32{100, 0}},
		// The leftover point goes to the LARGEST remainder, which here is
		// the last option — not simply to the first one in the list.
		"the leftover is not the first": {[]int32{2, 3, 1}, []int32{33, 50, 17}},
	}
	for name, tc := range cases {
		got := apportion(tc.counts)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
			continue
		}
		var sum int32
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v, want %v", name, got, tc.want)
				break
			}
			sum += got[i]
		}
		if len(tc.counts) > 0 && sum != 100 && sum != 0 {
			t.Errorf("%s: shares sum to %d, not 100", name, sum)
		}
	}
}

// TestApportionNeverExceedsTheWhole across many shapes, including the ones
// where every share has the same remainder and the leftovers have to go
// somewhere.
func TestApportionNeverExceedsTheWhole(t *testing.T) {
	shapes := [][]int32{
		{1, 1, 1, 1, 1, 1, 1},
		{1, 2, 3, 4, 5, 6, 7, 8, 9},
		{99, 1},
		{1, 99},
		{0, 0, 1},
		{6, 6, 6},
	}
	for _, counts := range shapes {
		var sum int32
		for _, share := range apportion(counts) {
			if share < 0 || share > 100 {
				t.Fatalf("%v: a share of %d%%", counts, share)
			}
			sum += share
		}
		if sum != 100 {
			t.Errorf("%v: shares sum to %d", counts, sum)
		}
	}
}

func TestWebPageCarriesWhatItHas(t *testing.T) {
	page := &tg.WebPage{URL: "https://lwn.net/Articles/1", DisplayURL: "lwn.net/Articles/1"}
	page.SetSiteName("LWN.net")
	page.SetTitle("Backpressure without queues")
	page.SetDescription("Why bounded channels beat unbounded buffers.")

	got := webPageFromTG(page)
	if got == nil {
		t.Fatal("a page with a title produced no preview")
	}
	if got.SiteName != "LWN.net" || got.Title != "Backpressure without queues" {
		t.Errorf("got %+v", *got)
	}
	if got.URL != "https://lwn.net/Articles/1" || got.DisplayURL != "lwn.net/Articles/1" {
		t.Errorf("urls = %+v", *got)
	}
}

// TestWebPageIsNilWhenItAddsNothing. Three wire shapes look different and
// render identically: a preview still being fetched, one Telegram gave up
// on, and one carrying only the URL the sender already typed.
func TestWebPageIsNilWhenItAddsNothing(t *testing.T) {
	bare := &tg.WebPage{URL: "https://example.com", DisplayURL: "example.com"}

	cases := map[string]tg.WebPageClass{
		"pending":     &tg.WebPagePending{},
		"empty":       &tg.WebPageEmpty{},
		"url only":    bare,
		"not fetched": &tg.WebPageNotModified{},
	}
	for name, page := range cases {
		if got := webPageFromTG(page); got != nil {
			t.Errorf("%s produced a preview: %+v", name, *got)
		}
	}
}

func TestWebPageSanitizesItsText(t *testing.T) {
	page := &tg.WebPage{URL: "https://x", DisplayURL: "x"}
	page.SetTitle("\x1b]0;pwned\x07title")

	got := webPageFromTG(page)
	if got == nil {
		t.Fatal("no preview")
	}
	if strings.ContainsRune(got.Title, 0x1b) {
		t.Fatalf("an escape survived: %q", got.Title)
	}
}

// TestWebPageMediaKeepsTheMessageText. The preview hangs off the text; it
// does not replace it.
func TestWebPageMediaKeepsTheMessageText(t *testing.T) {
	c := &Client{files: newFileRegistry()}
	page := &tg.WebPage{URL: "https://lwn.net", DisplayURL: "lwn.net"}
	page.SetTitle("Backpressure without queues")

	content := c.contentFromMedia(
		&tg.MessageMediaWebPage{Webpage: page},
		&FormattedText{Text: "Read later: Backpressure without queues"},
	)

	text, ok := content.(*MessageText)
	if !ok {
		t.Fatalf("content = %T, want *MessageText", content)
	}
	if text.Text == nil || text.Text.Text != "Read later: Backpressure without queues" {
		t.Fatalf("the message text was lost: %+v", text.Text)
	}
	if text.WebPage == nil || text.WebPage.Title != "Backpressure without queues" {
		t.Fatalf("the preview was lost: %+v", text.WebPage)
	}
}

// packWaveform is the encoder Telegram's is the decoder of: five bits per
// sample, least significant bit first, packed end to end.
func packWaveform(samples []byte) []byte {
	packed := make([]byte, (len(samples)*5+7)/8)
	for i, s := range samples {
		bit := i * 5
		index, shift := bit/8, bit%8
		packed[index] |= byte(uint16(s&0x1F) << shift)
		if index+1 < len(packed) {
			packed[index+1] |= byte(uint16(s&0x1F) >> (8 - shift))
		}
	}
	return packed
}

func TestDecodeWaveform(t *testing.T) {
	// Hand-computed, so the test cannot agree with a wrong decoder just
	// because packWaveform below is wrong the same way: samples 31, 0, 31
	// pack into bits 11111 00000 11111, which is 0x1F then 0x7C.
	if got := decodeWaveform([]byte{0x1F, 0x7C}); len(got) != 3 ||
		got[0] != 31 || got[1] != 0 || got[2] != 31 {
		t.Fatalf("got %v, want [31 0 31]", got)
	}

	for _, samples := range [][]byte{
		{0},
		{31, 0, 31},
		{1, 2, 3, 4, 5, 6, 7, 8},
		{0, 0, 0, 0, 0, 0, 0, 0, 31},
	} {
		got := decodeWaveform(packWaveform(samples))
		if len(got) < len(samples) {
			t.Fatalf("%v: decoded %d of %d samples", samples, len(got), len(samples))
		}
		for i, want := range samples {
			if got[i] != want {
				t.Fatalf("%v: sample %d = %d, want %d", samples, i, got[i], want)
			}
		}
	}
}

// TestDecodeWaveformDropsPadding. The bits left over at the end of the last
// byte are padding to the byte, not a quiet sample.
func TestDecodeWaveformDropsPadding(t *testing.T) {
	if got := decodeWaveform(nil); got != nil {
		t.Errorf("nil packed = %v, want nil", got)
	}
	// One byte holds one whole sample and three spare bits.
	if got := decodeWaveform([]byte{0xFF}); len(got) != 1 {
		t.Errorf("one byte decoded to %d samples, want 1", len(got))
	}
	// Every sample is within the five-bit range whatever the input.
	for _, s := range decodeWaveform([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}) {
		if s > 31 {
			t.Fatalf("sample %d is outside the five-bit range", s)
		}
	}
}

func TestVoiceNoteCarriesItsWaveform(t *testing.T) {
	c := &Client{files: newFileRegistry()}
	samples := []byte{0, 8, 16, 24, 31, 24, 16, 8}

	doc := &tg.Document{ID: 5, Attributes: []tg.DocumentAttributeClass{
		&tg.DocumentAttributeAudio{Voice: true, Duration: 47},
	}}
	audio := doc.Attributes[0].(*tg.DocumentAttributeAudio)
	audio.SetWaveform(packWaveform(samples))

	voice, ok := c.contentFromDocument(doc, nil).(*MessageVoiceNote)
	if !ok {
		t.Fatalf("a voice document did not produce a voice note")
	}
	if len(voice.VoiceNote.Waveform) < len(samples) {
		t.Fatalf("waveform = %v", voice.VoiceNote.Waveform)
	}
	for i, want := range samples {
		if got := voice.VoiceNote.Waveform[i]; got != want {
			t.Fatalf("sample %d = %d, want %d", i, got, want)
		}
	}
}

// TestVoiceNoteWithoutAWaveformHasNone. The sender's client may not have
// computed one, and an absent waveform must stay absent rather than become
// a flat line.
func TestVoiceNoteWithoutAWaveformHasNone(t *testing.T) {
	c := &Client{files: newFileRegistry()}
	doc := &tg.Document{ID: 5, Attributes: []tg.DocumentAttributeClass{
		&tg.DocumentAttributeAudio{Voice: true, Duration: 12},
	}}

	voice, ok := c.contentFromDocument(doc, nil).(*MessageVoiceNote)
	if !ok {
		t.Fatal("a voice document did not produce a voice note")
	}
	if voice.VoiceNote.Waveform != nil {
		t.Fatalf("waveform = %v, want nil", voice.VoiceNote.Waveform)
	}
}

// TestMessageEditedNeedsAChat. A reaction or a poll vote arrives as its own
// update, and this client answers both by refetching the message — which it
// can only do if the update says which chat the message is in.
func TestMessageEditedNeedsAChat(t *testing.T) {
	got, ok := messageEdited(&tg.PeerChannel{ChannelID: 42}, 7)
	if !ok {
		t.Fatal("an update naming a channel and a message produced no refetch")
	}
	if got.ChatId != channelChatID(42) || got.MessageId != 7 {
		t.Fatalf("got %+v", got)
	}

	for name, peer := range map[string]tg.PeerClass{
		"no peer at all": nil,
		"an empty user":  &tg.PeerUser{},
	} {
		if _, ok := messageEdited(peer, 7); ok {
			t.Errorf("%s produced a refetch aimed at chat zero", name)
		}
	}
	if _, ok := messageEdited(&tg.PeerChannel{ChannelID: 42}, 0); ok {
		t.Error("an update naming no message produced a refetch")
	}
}

// TestAMessagesReactionsAreMapped covers the wiring, not the conversion:
// a message can carry tallies and drop them on the way into the domain.
func TestAMessagesReactionsAreMapped(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	m := &tg.Message{
		ID:      9,
		PeerID:  &tg.PeerUser{UserID: 4},
		Message: "hello",
	}
	m.SetReactions(tg.MessageReactions{Results: []tg.ReactionCount{
		{Reaction: &tg.ReactionEmoji{Emoticon: "👍"}, Count: 3},
	}})

	msg := c.messageFromTG(m)
	if len(msg.Reactions) != 1 {
		t.Fatalf("got %d reactions, want 1", len(msg.Reactions))
	}
	if msg.Reactions[0].Emoji != "👍" || msg.Reactions[0].Count != 3 {
		t.Fatalf("reaction = %+v", *msg.Reactions[0])
	}

	plain := c.messageFromTG(&tg.Message{ID: 9, PeerID: &tg.PeerUser{UserID: 4}, Message: "hi"})
	if plain.Reactions != nil {
		t.Fatalf("a message nobody reacted to has %+v", plain.Reactions)
	}
}

// TestAPollMessageCarriesItsResults covers the same wiring for a poll: the
// media conversion has both halves in hand and can pass on only one.
func TestAPollMessageCarriesItsResults(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	results := tg.PollResults{Results: []tg.PollAnswerVoters{{Option: []byte("a"), Voters: 4}}}
	results.SetTotalVoters(4)

	content := c.contentFromMedia(&tg.MessageMediaPoll{
		Poll: tg.Poll{
			Question: tg.TextWithEntities{Text: "Ship?"},
			Answers:  []tg.PollAnswerClass{answer("Yes", "a")},
		},
		Results: results,
	}, nil)

	poll, ok := content.(*MessagePoll)
	if !ok {
		t.Fatalf("content = %T, want *MessagePoll", content)
	}
	if len(poll.Poll.Options) != 1 || poll.Poll.Options[0].VoterCount != 4 {
		t.Fatalf("the tallies were dropped: %+v", poll.Poll)
	}
	if poll.Poll.TotalVoterCount != 4 {
		t.Fatalf("total = %d", poll.Poll.TotalVoterCount)
	}
}

// TestAMultipleChoiceTotalIsNotTheSumOfItsAnswers. Three people picking two
// options each cast six votes, and a footer reading "6 votes" over a poll
// three people answered is a number this client made up.
func TestAMultipleChoiceTotalIsNotTheSumOfItsAnswers(t *testing.T) {
	results := tg.PollResults{Results: []tg.PollAnswerVoters{
		{Option: []byte("a"), Voters: 3},
		{Option: []byte("b"), Voters: 3},
	}}
	results.SetTotalVoters(3)

	poll := pollFromTG(tg.Poll{
		Question:       tg.TextWithEntities{Text: "Pick any"},
		MultipleChoice: true,
		Answers:        []tg.PollAnswerClass{answer("A", "a"), answer("B", "b")},
	}, results)

	if poll.TotalVoterCount != 3 {
		t.Fatalf("total = %d, want 3 — six votes were cast by three people",
			poll.TotalVoterCount)
	}
	// The shares are still of the votes cast, which is what the bars compare.
	for i, option := range poll.Options {
		if option.Percent != 50 {
			t.Errorf("option %d = %d%%, want 50", i, option.Percent)
		}
	}
}
