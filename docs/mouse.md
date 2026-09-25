# Mouse and selection

What a click and the wheel do in this client, and how to get text out of it.

## Why dragging selects nothing

The client asks the terminal for mouse reporting — `MouseModeCellMotion`,
which every frame asks for — and so holds it for the whole session, because
clicks and the wheel do something here. While that is on, the terminal
forwards mouse events to this program instead of acting on them itself, and
its own text selection is one of the things it stops doing. The drag arrives
as app input, the app does nothing with it, and nothing lands on your
clipboard.

It is the same trade
[Clicking a link does nothing](troubleshooting.md#clicking-a-link-does-nothing)
describes for hyperlinks, and the way round it is the same.

## Hold Shift and drag

Almost every terminal reserves a modifier that bypasses mouse reporting and
selects natively over the top of whatever is running: **Shift** in kitty,
Ghostty, WezTerm, foot, xterm and the VTE family (GNOME Terminal, Tilix);
iTerm2 uses Cmd. Hold it, drag, and you get your terminal's ordinary
selection with whatever copy gesture it normally has. Which modifier it uses
is your terminal's decision rather than this client's, and its manual is the
authority.

Some terminals also offer a rectangular, or block, selection. Where it exists
it is the better shape for this frame — see below — but it is a different
modifier or a config flag in each one that has it, so this page can point you
at your terminal's documentation and not at a keystroke.

## The selection takes whole rows

Your terminal is selecting cells, not messages: it knows where the glyphs
are, and nothing about the frame drawn out of them. An ordinary drag takes
every row between its two ends, whole, so everything else on those rows comes
along — the chat list down the left, the vertical rule between the columns,
the context rail on the right when it is open, and the folder tabs or the
hint bar if the drag reaches the top or bottom row. Inside the thread, a
message's first row carries its timestamp and sender, and each row after it
carries the blank gutter where those were.

Three things narrow it. A **block selection**, where your terminal has one,
takes a column range instead of whole rows, which is the shape a panel is. A
**narrower terminal** has less to take: below 72 columns one panel owns the
full width and `Tab` swaps which, the rail is only drawn at 118 columns and
wider, and `` ` `` turns the rail off at any width. And `y` sidesteps the
geometry entirely.

## `y` copies the message

`y` on the cursored message puts its text on the system clipboard, and for
copying what somebody wrote it is the better tool. It takes the message's
text **as Telegram sent it** rather than the rendering: no gutter, no
timestamp, no wrap at the pane width, and a code block as the code instead of
as the frame around it — so a message that is nothing but a code block yanks
as the code. It reports `copied N characters`, or `nothing to copy — this
message has no text` for an uncaptioned photo, and names the failure
otherwise.

It reaches the clipboard through the platform's own helper — `pbcopy` on
macOS, `wl-copy`, `xclip` or `xsel` on Unix, `clip.exe` on Windows — so a
machine with none of them says so rather than appearing to work, and over
plain SSH it fills the clipboard of the machine the client is running on,
which is not the one in front of you. Shift+drag is the route that always
ends in your own clipboard, because your terminal never left the loop.

## What the mouse does here

The left button only; a right or middle click is ignored. Nothing drags —
motion and release are never acted on, which is why the terminal's own
selection is the only selection there is.

| Click on | What happens |
|---|---|
| a folder tab | Switches to that folder. The tabs are the top bar's, and the rest of that row is inert |
| the chat list | Focuses the column, moves the highlight to that row and opens the chat on it. The filter header is inert, and inside a forum the highlight moves without opening the topic |
| the contacts overlay | Focuses it and opens a private chat with the contact on that row — it borrows the same column |
| the thread | Focuses the chat view and moves the message cursor to the message under the pointer, which is then what `r`, `y`, `+` and the rest act on. A day divider, the header, and the blank rows above a history shorter than the pane move nothing |
| the composer | Focuses it. The text cursor stays where it was |
| the hint bar | Nothing. It is not a click target |

| Wheel over | What happens |
|---|---|
| the chat list | Moves the cursor one row per notch and opens nothing — the same cursor `j`/`k` move — and asks for the next page of chats as it nears the end, so a mouse user is not stopped at the first page |
| the contacts overlay | Scrolls it one row per notch |
| the thread | Scrolls three lines per notch, and starts the thumbnail downloads the move brought near the viewport |

While the `?` help card is open both are dropped rather than routed to the
panels behind it: the card has nothing clickable, and it scrolls from the
keyboard. The sign-in steps and the opening loading screen act on no mouse
input at all, though they still ask for it — Shift is the way to select there
too. The one screen that asks for none is the error panel that replaces the
UI when the Telegram client has died for good, so the terminal's selection
comes back by itself on the one screen whose text you are most likely to want
to copy.

Everything the mouse does, the keyboard does — see
[Keybindings](keys.md). The reverse is deliberately not true, and
[decision I-11](interaction-model.md) is why a click in the thread moves the
cursor rather than only focusing the panel: a mouse user who could focus the
thread but not choose a message had nothing for `r`, `y` or `+` to act on.
