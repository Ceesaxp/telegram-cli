package chatlist

import "github.com/Ceesaxp/telegram-cli/internal/telegram"

// ChatSelectedMsg is emitted when the user selects a chat.
type ChatSelectedMsg struct {
	ChatId int64
}

// TopicSelectedMsg is emitted when the user selects a topic, while the list
// is drilled into a forum (see [Model.EnterForum]).
//
// It carries the topic and NOT a chat ID, deliberately. A topic is a chat to
// everything above internal/telegram, but the ID it is that chat under is
// minted by the client's topic registry (docs/topics.md, "The model") and
// this component has no business knowing it — it would have to hold a
// client to ask, which is the dependency the drill-in is built without. The
// app translates: it knows which forum is open, because it opened it.
type TopicSelectedMsg struct {
	Topic *telegram.Topic
}

// There is deliberately no filter message.
//
// One existed, announcing every filter change to the app layer, and nothing
// ever handled it — the filter is applied by refreshList before the command
// is even returned, so the announcement had nothing left to cause.
//
// The app does not need telling. refreshChrome re-derives the chrome from
// current state on every tick and every layout change, and this component
// exposes FilterQuery, Count and TotalCount for it to read. A message would
// be edge-triggered, so the app would have to STORE what it was told — a
// second copy of state this component already owns, which is how a chat came
// to be unmuted by opening it (divergence 39).
