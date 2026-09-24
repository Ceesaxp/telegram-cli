# Forum topics — spec

Status: **built** (waves 0–2, 2026-09-24). The protocol facts in "What
Telegram actually does" were researched against core.telegram.org/api/forum,
TDLib and gotd v0.161 and are cited. "As built" at the end records what the
implementation decided where this document left room, and what is still
open.

A forum is a supergroup whose messages are filed under topics. tele-tui
today sees only the flat stream: every message from every topic, in one
thread, and a `t.me/<group>/<topic>/<id>` link is refused because there is
nowhere to land (`internal/telegram/tme.go`). This document decides what
topic support is, how it is modelled, and in what order it is built.

## The three questions this answers

1. **Where do topics appear?** In the chat list, one level down.
2. **What is a topic, to the rest of the client?** A chat.
3. **What is built first?** A bug fix that helps even if topics are never
   built, then reading, then posting.

## What Telegram actually does

Each fact below is what the implementation must match. Sources are the
forum API page unless stated.

- **A forum is a supergroup with `channel.forum` set.** Not a field on
  `channelFull`; gotd `tg.Channel.Forum`. A separate flag,
  `channel.forum_tabs`, is only a hint about how official clients draw the
  picker, and `channelFull.view_forum_as_messages` is a per-account
  preference, synced between devices, for reading a forum flat.
- **A topic is a message thread, and its ID is a message ID**: the
  `messageActionTopicCreate` service message that created it. **General is
  topic 1** and cannot be deleted; only General can be hidden.
- **A message's topic comes from its reply header.** `messageReplyHeader`
  carries `forum_topic` and `reply_to_top_id`. TDLib's rule
  (`MessageReplyHeader.cpp`, `MessageTopic.cpp`): if the flag is set, the
  topic is `reply_to_top_id`, with the special case that a message which is
  not itself a reply reports the topic in `reply_to_msg_id`; if the flag is
  absent in a forum, the message is in **General**. So in a forum, every
  ordinary message carries a reply header that is *not* a reply.
- **A topic's history is `messages.getReplies` with `msg_id` = the topic
  ID** (TDLib `MessagesManager.cpp`), paged like any history.
- **Listing topics is `messages.getForumTopics`**, which returns
  `forumTopic` records — title, `icon_color`, `icon_emoji_id`,
  `top_message`, `unread_count`, `unread_mentions_count`,
  `unread_reactions_count`, `read_inbox_max_id`, the `closed`, `pinned`,
  `hidden` and `my` flags, per-topic `notify_settings` and a per-topic
  `draft` — plus the messages, users and chats needed to render them. It
  pages by `offset_date` / `offset_id` / `offset_topic`, and
  `order_by_create_date` in the response says which date to page by.
  `messages.getForumTopicsByID` refreshes named topics and answers
  `forumTopicDeleted` for the ones that are gone.
- **Reading a topic is `messages.readDiscussion`** (peer, topic ID, max
  ID), and the server echoes `updateReadChannelDiscussionInbox` /
  `Outbox`, which carry `top_msg_id` and the read pointer.
- **Per-topic unread counts never arrive in an update.** The read updates
  carry no count — TDLib passes `-1` for it and keeps its own — so a count
  is either derived locally from messages newer than the read pointer, or
  re-fetched with `getForumTopics`.
- **Topic creation, edit and deletion have no updates either.** They
  arrive as ordinary new/edited service messages
  (`messageActionTopicCreate` / `messageActionTopicEdit`), and a deletion
  arrives as a message deletion that includes the topic's root message.
  Pinning is the exception: `updatePinnedForumTopic(s)`.
- **Posting into a topic** sets `inputReplyToMessage.reply_to_msg_id` to
  the topic ID. Replying to a message inside a topic sets
  `reply_to_msg_id` to that message and `top_msg_id` to the topic.
  General takes neither: a plain send lands there.
- **Whether the account may post in a closed topic is decidable locally**
  (TDLib `ForumTopicManager::can_send_message_to_forum_topic`): a closed
  topic refuses everyone except its creator (`my`) and anyone holding
  `manage_topics`. The server's backstop is the `TOPIC_CLOSED` error;
  gotd has `tg.IsTopicClosed`, `IsTopicDeleted`, `IsTopicIDInvalid`.
- **Monoforums are not topics.** gotd v0.161 also carries
  `channel.monoforum` and `updateReadMonoForumInbox/Outbox`, which are
  direct messages to channels. Nothing here applies to them, and the code
  must not conflate the two.

Unverified, and therefore not relied on: whether General can be closed or
renamed, and whether a topic-less send can ever be refused rather than
landing in General.

## The interaction

**A forum opens like a directory.** `Enter` on a forum row replaces the
chat list's contents with that forum's topics; a back key returns to the
chats. It is the same panel, the same two-row cells, the same filter, one
level down — a file manager, not a new column. The frame's width rules
(`docs/tui-2.0.md`) give the thread the space that a third column would
have taken, and at 72 columns there is no room for one at all.

```
 ‹ Go Serbia            3/12 │ Go Serbia › Jobs
▌# General         14:02     │ …
▌  ana: meetup Thursday?     │
 # Jobs        •3  13:40     │
   milos: remote Go role …   │
```

- **The header row** replaces the filter's count line with the forum's
  name and a back hint, and the filter keeps working over topic titles.
- **A topic row** is a chat row: title, time, last message, unread badge,
  the `@` chip when it has unread mentions. Pinned topics sort first, as
  pinned chats do. A closed topic is marked; a hidden General is not
  listed.
- **The topic's colour.** `icon_color` is one of six fixed values, which
  is exactly what the sender-colour ramp already does with a hash — the
  topic sigil takes its colour from `icon_color` rather than inventing
  one. A custom `icon_emoji_id` is a premium emoji and is **not**
  rendered in the first wave; the sigil stays `#`.
- **Opening a topic** opens the thread scoped to it. The thread header
  reads `Forum › Topic`, so the reader always knows which of the two they
  are in.
- **The back key** cannot be `h`: the app claims it before any panel sees
  it, for moving focus from the thread back to the chat list
  (`internal/keys/reserved.go`, `app.go`'s browsing dispatch), and folders
  cycle on `[`/`]`. Decision: **`Esc`**, with `Backspace` as a synonym.
  `Esc` in the chat list already clears an applied filter, so it stacks:
  with a filter up, the first `Esc` clears it and the second leaves the
  forum. `Backspace` is bound to nothing there and always goes up.
- **`:topic <name>`** jumps within the open forum with the palette's
  fuzzy completion, exactly as `:theme` now lists themes. It is an
  accelerator, not the way in: it cannot show which topics have unread
  messages, which is the whole reason the list exists.
- **Reading a forum flat.** The account-level `view_forum_as_messages`
  preference is honoured when it is set: `Enter` opens the flat stream and
  the topic list is one keystroke away. tele-tui does not offer to change
  the preference in the first wave.
- **A closed topic** is marked on its row, and `TOPIC_CLOSED` from the
  server is reported as a readable send failure rather than a crash.
  Gating the composer on the local rule above is **not built** — see the
  follow-ups in `TODO.md`.

## The model: a topic is a chat with a synthetic ID

Everything in this client is keyed by chat ID: the message store and its
index, the chat store's unread counts, per-chat drafts, the jump stack,
read receipts, notifications, `GetChatHistory`, `SendTextMessage`,
`ViewMessages`. A topic needs every one of those, per topic.

Two ways to get there:

1. **Thread a topic ID through everything.** Honest, and it changes the
   signature of nearly every store method, every client call and every
   chat-view field, plus their tests.
2. **Give each topic a synthetic chat ID**, and translate at the one
   boundary where the app talks to Telegram.

**Decision: (2).** A topic already behaves like a chat in every way the UI
cares about — its own unread count, draft, read state, row and history —
so the layers above the client need no new concept, and the change lands
almost entirely in `internal/telegram`.

The rules that make it safe:

- **The band.** Synthetic IDs are allocated from a reserved range that no
  TDLib peer ID can occupy, by a registry inside `internal/telegram` that
  maps `synthetic ↔ (channelID, topicID)` both ways. Allocation is
  session-scoped and sequential; nothing persists it, and nothing outside
  the client interprets it.
- **The chokepoint.** Every exported `Client` method that takes a chat ID
  begins by splitting it: `real, topic := c.split(chatID)`. Methods that
  need the topic use it (history, send, read); the rest carry on with
  `real`.
- **The guard.** `inputPeer` refuses a synthetic ID outright, so a peer
  lookup for one can never reach the wire. A test asserts that every
  exported method either splits or refuses, walking the package's AST the
  way the palette guard walks components.
- **What the layers above see** is a chat that happens not to be in the
  dialog list: a title from the topic, `Type` supergroup, its own unread
  count. `GetChat` on a synthetic ID answers from the topic registry
  rather than the server.

The cost of the decision, recorded honestly: two IDs now mean the same
conversation to a human (`the forum`, `the forum's General`), and any
future feature that persists a chat ID — a saved layout, a session
restore — must resolve synthetic IDs first or refuse them.

## Waves

**Wave 0 — the reply bug (worth doing even if topics never are).**
In a forum, every message carries a reply header pointing at the topic
root, and `messageFromTG` copies it into `ReplyToMessageID`
(`internal/telegram/types.go`). The thread therefore draws a reply quote
under every message, almost always "earlier message" because the root is
not loaded. Parse the header properly: record `TopicID` on the message,
and set `ReplyToMessageID` only for a real reply. Small, self-contained,
and it makes forums readable today.

**Wave 1 — reading.** Forum detection, the topic registry and the split
chokepoint, `ForumTopics(chatID)` over `getForumTopics` with paging, the
drill-in list with its header and back key, per-topic history through
`getReplies`, unread counts from the topic records and derived from
arriving messages, routing an incoming message to its topic's store.

**Wave 2 — posting and reading marks** (ships with wave 1). Sending with the right reply
header (plain post, and reply inside a topic), `readDiscussion` on open
and on arrival, handling `updateReadChannelDiscussionInbox/Outbox`, the
closed-topic rule and the `TOPIC_CLOSED` error, drafts per topic.

**Wave 3 — polish** (not built). `:topic` with fuzzy completion, `t.me/<group>/<topic>/<id>`
links (the refusal in `tme.go` goes away), pinned ordering and
`updatePinnedForumTopic(s)`, topic create/edit/delete service messages
refreshing the list via `getForumTopicsByID`, per-topic mention and
reaction counts.

**Out of scope, recorded.** Creating, renaming, closing, pinning or
deleting topics from tele-tui; monoforums; toggling `view_forum_as_messages`;
per-topic notification settings; custom emoji topic icons.

## Risks

- **A synthetic ID reaching the wire.** Mitigated by the `inputPeer`
  refusal and the AST guard; a leak would produce a confusing server error
  rather than a wrong-chat send, because no real peer has that ID.
- **Unread counts drifting.** They are derived locally between refreshes,
  the same trade-off the chat list already makes, with the same repair:
  a re-fetch on reconnect (`getForumTopicsByID` for the open forum).
- **A forum with hundreds of topics.** The list pages like the dialog
  list does; the filter is the way to reach a topic by name, and `:topic`
  is the accelerator.
- **Message IDs are the channel's, not the topic's.** Two topics never
  hold the same message, so a per-topic store keyed by message ID stays
  consistent; the ID index added in the performance wave is unaffected.

## Resolved (2026-09-23)

1. **The back key is `Esc`, with `Backspace` as a synonym.** `Esc` clears
   an applied filter first and leaves the forum on the next press;
   `Backspace` is unbound in the chat list and always goes up. `h` stays
   panel movement.
2. **The thread shows the last topic read in that forum**, and the flat
   stream only on first entry, before any topic in it has been opened.
3. **Waves 1 and 2 ship together.** Reading a topic while replies land in
   General would be worse than today's flat view, so posting is not
   deferred to a later release.

## As built

The waves landed on `feat/forum-topics`; `git log --oneline` there is the
record. What the implementation decided, beyond what is above:

- **The band starts at `1<<48`.** Every `constant.TDLibPeerID` range is
  either negative or ends at `MaxTDLibUserID` = `(1<<40)-1`, so a synthetic
  ID answers false to `IsUser`, `IsChat`, `IsChannel` and `IsMonoforum`.
  That matters because `ViewMessages`, `DeleteMessages`, `GetMessages` and
  `ReadMentions` pick their RPC on `IsChannel`: an ID that answered yes
  would be routed as a channel that does not exist, while one that answers
  no takes the peer path, where `inputPeer` refuses it by name.
- **Publish under the ID the caller used.** Anything this package announces
  about a topic — a sent message's echo, an arriving copy, a read mark, a
  reaction — carries the synthetic ID, because that is what the store, the
  row and the open thread are keyed by. `publishSent`, `messagesFiledIn`,
  `messageFiledUnderItsTopic` and `chatsForUpdate` are where it happens.
- **An arriving message is announced twice**, once under the forum and once
  under the topic, and only for a forum whose topics this session has
  listed: the forum's own row and flat stream still want it, and a forum
  nobody opened has nothing keyed by its topics. An edit, a reaction, a
  poll vote and a typing notice are announced the same way — only one of
  the two chats can be the open one, so the other is a switch that does
  not match. A remote deletion carries no topic in the schema, so it goes
  to the forum and to every listed topic of it; a channel message ID names
  one message in one topic, so it is a no-op everywhere else.
- **The guard keeps two lists.** A method that takes a chat ID must
  translate it; refusing is allowed only as a listed, argued decision.
  Accepting "it reaches `inputPeer`, which refuses" as handled is what hid
  reactions, pinning, editing and forwarding being broken inside a topic
  until late in the work.
- **A read from another device is published as a mark, not a read-inbox.**
  The discussion updates carry no count — TDLib passes -1 and keeps its own
  — and a read-inbox carries one the store believes, so a phone-side read
  of three old messages would have zeroed the badge.
- **The topic sigil** takes Telegram's six icon colours through the
  palette's roles; pink and red share the one warm red rather than
  inventing a sixth. A closed topic is marked the way a muted chat is.

Still open, recorded in `TODO.md`: per-topic notification settings (a topic
silenced on the phone still rings here), `view_forum_as_messages`, custom
emoji topic icons, `u` walking a forum's unread topics, draft marks on
topic rows, and wave 3 in full.
