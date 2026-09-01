package telegram

import (
	"cmp"
	"slices"

	"github.com/gotd/td/tg"
)

// This file maps the four content blocks the design record draws and the
// domain model had no room for: reactions, poll results, link previews and
// a voice note's waveform. Each is a translation and nothing more — what
// the server did not send is left absent rather than defaulted, because a
// renderer cannot tell a real zero from a filled-in one.

// reactionsFromTG converts a message's reaction tallies.
//
// A reaction whose type this client does not understand is DROPPED rather
// than shown as a blank chip: the count would be real and the thing counted
// unknown, which reads as a rendering fault. Custom emoji survive, because
// their chip can say honestly that it cannot show the artwork.
func reactionsFromTG(r tg.MessageReactions) []*Reaction {
	out := make([]*Reaction, 0, len(r.Results))
	for _, rc := range r.Results {
		reaction := &Reaction{Count: int32(rc.Count)}
		if _, ok := rc.GetChosenOrder(); ok {
			reaction.Chosen = true
		}
		switch v := rc.Reaction.(type) {
		case *tg.ReactionEmoji:
			reaction.Emoji = sanitizeTerminal(v.Emoticon)
		case *tg.ReactionCustomEmoji:
			reaction.CustomEmojiID = v.DocumentID
		default:
			continue
		}
		out = append(out, reaction)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// pollFromTG converts a poll and its results into the domain type.
func pollFromTG(p tg.Poll, r tg.PollResults) *Poll {
	poll := &Poll{
		Question:       sanitizeTerminal(p.Question.Text),
		IsAnonymous:    !p.PublicVoters,
		IsClosed:       p.Closed,
		MultipleChoice: p.MultipleChoice,
		IsQuiz:         p.Quiz,
	}
	if closeDate, ok := p.GetCloseDate(); ok {
		poll.CloseDate = int32(closeDate)
	}
	total, _ := r.GetTotalVoters()
	poll.TotalVoterCount = int32(total)

	// Tallies come back keyed by each answer's opaque option bytes, in
	// their own order, and a poll whose results are hidden sends none at
	// all. Index them and look each answer up, rather than zipping two
	// lists that are not promised to line up.
	voters := make(map[string]tg.PollAnswerVoters, len(r.Results))
	for _, v := range r.Results {
		voters[string(v.Option)] = v
	}

	counts := make([]int32, 0, len(p.Answers))
	for _, a := range p.Answers {
		answer, ok := a.(*tg.PollAnswer)
		if !ok {
			continue
		}
		option := &PollOption{Text: sanitizeTerminal(answer.Text.Text)}
		if v, ok := voters[string(answer.Option)]; ok {
			option.VoterCount = int32(v.Voters)
			option.Chosen = v.Chosen
			option.Correct = v.Correct
			poll.ResultsKnown = true
		}
		poll.Options = append(poll.Options, option)
		counts = append(counts, option.VoterCount)
	}

	if poll.ResultsKnown {
		for i, percent := range apportion(counts) {
			poll.Options[i].Percent = percent
		}
	}
	return poll
}

// apportion turns vote counts into whole percentages summing to exactly
// 100, by largest remainder.
//
// Rounding each share on its own is what produces three options reading
// 64%, 27% and 9% above a footer that says 11 votes — or 33/33/33, where a
// reader can only conclude the client cannot count. The leftover points go
// to the largest remainders, ties to the earlier option, so the same poll
// always apportions the same way.
//
// The result is all zeroes when nobody has voted, which is the truth: no
// option has a share of nothing.
func apportion(counts []int32) []int32 {
	out := make([]int32, len(counts))

	var total int64
	for _, c := range counts {
		if c > 0 {
			total += int64(c)
		}
	}
	if total == 0 {
		return out
	}

	type share struct {
		index     int
		remainder int64
	}
	var (
		assigned int32
		shares   []share
	)
	for i, c := range counts {
		if c <= 0 {
			continue
		}
		scaled := int64(c) * 100
		out[i] = int32(scaled / total)
		assigned += out[i]
		shares = append(shares, share{index: i, remainder: scaled % total})
	}

	slices.SortStableFunc(shares, func(a, b share) int {
		return cmp.Compare(b.remainder, a.remainder)
	})
	for i := 0; assigned < 100 && i < len(shares); i++ {
		out[shares[i].index]++
		assigned++
	}
	return out
}

// webPageFromTG converts a link preview, or reports that there is nothing
// to draw.
//
// nil covers three cases that look different on the wire and identical on
// screen: a preview Telegram is still fetching, one it has given up on, and
// one carrying nothing but the URL the sender already typed. Drawing a rule
// beside a bare link would add a block that says less than the line above it.
func webPageFromTG(w tg.WebPageClass) *WebPage {
	page, ok := w.(*tg.WebPage)
	if !ok {
		return nil
	}

	siteName, _ := page.GetSiteName()
	title, _ := page.GetTitle()
	description, _ := page.GetDescription()

	preview := &WebPage{
		URL:         sanitizeTerminal(page.URL),
		DisplayURL:  sanitizeTerminal(page.GetDisplayURL()),
		SiteName:    sanitizeTerminal(siteName),
		Title:       sanitizeTerminal(title),
		Description: sanitizeTerminal(description),
	}
	if preview.SiteName == "" && preview.Title == "" && preview.Description == "" {
		return nil
	}
	return preview
}

// waveformBits is how many bits Telegram spends on one amplitude sample.
const waveformBits = 5

// waveformMax is the largest amplitude those bits can hold.
const waveformMax = 1<<waveformBits - 1

// decodeWaveform unpacks a voice note's waveform: amplitudes of five bits
// each, packed back to back, least significant bit first.
//
// The samples are returned at their own scale, 0–31, not rescaled to any
// number of rows. Whoever draws the bar knows how tall it is; this knows
// only what the sender measured.
//
// Trailing bits that cannot make a whole sample are dropped — they are
// padding to the byte, not a quiet sample at the end.
func decodeWaveform(packed []byte) []byte {
	samples := len(packed) * 8 / waveformBits
	if samples == 0 {
		return nil
	}

	out := make([]byte, 0, samples)
	for i := range samples {
		bit := i * waveformBits
		index, shift := bit/8, bit%8

		value := uint16(packed[index]) >> shift
		if index+1 < len(packed) {
			value |= uint16(packed[index+1]) << (8 - shift)
		}
		out = append(out, byte(value&waveformMax))
	}
	return out
}
