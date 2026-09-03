package chatlist

// ChatSelectedMsg is emitted when the user selects a chat.
type ChatSelectedMsg struct {
	ChatId int64
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
