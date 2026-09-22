package telegram

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// OpenAllowedSendFile opens path for a remote caller's send if it names a
// regular file inside one of roots. The send then reads that descriptor
// (see [Client.SendOpenedFileMessage]) and never goes back to the path.
//
// That is the point of it. Checking a path and then opening it by name
// leaves a gap in which anyone who can write to a root swaps the checked
// file for a symlink to one outside, and the upload reads that instead.
// Here the check is the open: path is made absolute and clean but not
// resolved, taken relative to each root in turn, and opened through an
// [os.Root], which follows a link only while it stays inside. Whatever the
// name points at afterwards, what is sent is what was opened.
//
// Blank roots are skipped, and with no usable root everything is refused.
// A root is matched as written and where it really is (see rootDirs), so
// a root under /tmp on macOS also matches the same path under
// /private/tmp. A link inside a root must be relative and stay inside:
// os.Root refuses an absolute link even when it names a file in the root.
func OpenAllowedSendFile(path string, roots ...string) (*os.File, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("send file: %w", err)
	}
	// found is why a root that holds the path could not send it. It
	// outranks "outside": the path was inside, and saying otherwise would
	// send the caller looking for a typo that is not there.
	var found error
	for _, dir := range rootDirs(roots) {
		rel, ok := within(dir, abs)
		if !ok {
			continue
		}
		f, err := openInRoot(dir, rel)
		if err == nil {
			return f, nil
		}
		if found == nil && foundButUnsendable(err) {
			found = fmt.Errorf("send file: %q: %w", path, cause(err))
		}
	}
	if found != nil {
		return nil, found
	}
	// Name the roots. The caller here is an authenticated operator or the
	// agent they configured, the set is already logged at startup and
	// documented, and "outside the allowed directories" with no list is a
	// dead end — the reader cannot tell a typo from a policy.
	return nil, fmt.Errorf("send file: path %q is outside the allowed directories (%s)",
		path, strings.Join(nonEmpty(roots), ", "))
}

// errNotRegular refuses what is not a plain file: a directory, a fifo, a
// device. There is nothing in one to send, and reading some of them has
// side effects or never ends.
var errNotRegular = errors.New("not a regular file")

// openInRoot opens name, relative to dir, without leaving dir, and only if
// it is a regular file. An [os.Root] refuses a ".." or a symlink that
// points outside it, checking each link as it follows it on the
// descriptors it opens; the type is then asked of the descriptor, not of
// the path, which may name something else by now.
//
// The root is closed on the way out. The file is not tied to it and stays
// open, which is what [os.OpenInRoot] relies on too.
func openInRoot(dir, name string) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	f, err := root.OpenFile(name, sendOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errNotRegular
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// foundButUnsendable reports whether err, from opening a path inside a
// root, says the file is there (or ought to be) and cannot be sent, as
// opposed to lying outside. Anything else — above all os.Root's refusal
// of a path that leaves it, which has no exported error of its own — reads
// as outside.
func foundButUnsendable(err error) bool {
	return errors.Is(err, errNotRegular) ||
		errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, fs.ErrPermission)
}

// cause is err without the path os.Root put on it, which is relative to
// the root; the caller's error names the path as they gave it instead.
func cause(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

// rootDirs is every directory a path may be named under: each root as
// written, made absolute, and also where it really is when that differs.
// A root is configuration, not something a caller can swap, so resolving
// its links here is safe in a way that resolving the file's would not be.
// Blank roots are skipped, as is one that cannot be made absolute.
func rootDirs(roots []string) []string {
	dirs := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		dirs = append(dirs, abs)
		if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
			dirs = append(dirs, real)
		}
	}
	return dirs
}

// within returns path relative to dir, if path is dir or lies beneath it.
// Both must be absolute and clean. It reads nothing from disk: a symlink
// on the way is the open's business, not this.
func within(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// nonEmpty drops the blank roots OpenAllowedSendFile skips, so the
// error names the set that was actually searched. A caller that passes no
// usable root gets "()" and rejects everything, which is the correct
// fail-closed reading of an empty allowlist.
func nonEmpty(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

// SendFileMessage uploads a local file and sends it as a document,
// optionally with a caption and as a reply.
//
// placeholderID is the local echo the caller already drew for this send, so
// the success message can name the row to swap; 0 when there is none.
func (c *Client) SendFileMessage(chatID int64, path, caption string, replyToMessageID int64, placeholderID int64) (*Message, error) {
	msg, _, err := c.SendFileMessageWithMentions(chatID, path, caption, nil, replyToMessageID, placeholderID)
	return msg, err
}

// SendFileMessageWithMentions is SendFileMessage for a caption that
// mentions users without a username (see MentionSpan). Dropped mentions
// are counted as in SendTextMessageWithMentions.
func (c *Client) SendFileMessageWithMentions(chatID int64, path, caption string, mentions []MentionSpan, replyToMessageID int64, placeholderID int64) (*Message, int, error) {
	ctx, cancel := transferCtx()
	defer cancel()

	peer, inputFile, err := c.uploadForSend(ctx, chatID, path)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: %w", err)
	}

	msg, dropped, err := c.sendUploadedMedia(ctx, peer, documentMedia(inputFile, path), caption, mentions, replyToMessageID, placeholderID)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: %w", err)
	}
	return msg, dropped, nil
}

// SendOpenedFileMessage is SendFileMessage for a file that is already open,
// normally by [OpenAllowedSendFile]: it uploads from f and never opens
// anything by name, so what is sent is what was checked. f is closed when
// the send is over, whether or not it succeeded.
func (c *Client) SendOpenedFileMessage(chatID int64, f *os.File, caption string, replyToMessageID int64, placeholderID int64) (*Message, error) {
	msg, _, err := c.SendOpenedFileMessageWithMentions(chatID, f, caption, nil, replyToMessageID, placeholderID)
	return msg, err
}

// SendOpenedFileMessageWithMentions is SendOpenedFileMessage for a caption
// that mentions users without a username, as SendFileMessageWithMentions
// is for a send by path.
//
// The chat shows the file under the base of the name it was opened by,
// which for [OpenAllowedSendFile] is the path the caller asked for — a
// link's own name, not its target's.
func (c *Client) SendOpenedFileMessageWithMentions(chatID int64, f *os.File, caption string, mentions []MentionSpan, replyToMessageID int64, placeholderID int64) (*Message, int, error) {
	defer f.Close()
	ctx, cancel := transferCtx()
	defer cancel()

	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: %w", err)
	}
	name := filepath.Base(f.Name())
	inputFile, err := c.uploadOpened(ctx, f, name)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: upload %q: %w", name, err)
	}

	msg, dropped, err := c.sendUploadedMedia(ctx, peer, documentMedia(inputFile, name), caption, mentions, replyToMessageID, placeholderID)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: %w", err)
	}
	return msg, dropped, nil
}

// uploadOpened puts f on Telegram's servers as name, reading only from f.
// The size comes from the descriptor as well, never from a path.
func (c *Client) uploadOpened(ctx context.Context, f *os.File, name string) (tg.InputFileClass, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return c.newUploader().Upload(ctx, uploader.NewUpload(name, f, info.Size()))
}

// documentMedia is an uploaded file as a document named for path: its base
// name is the file name the chat shows, and its extension picks the MIME
// type.
func documentMedia(file tg.InputFileClass, path string) *tg.InputMediaUploadedDocument {
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return &tg.InputMediaUploadedDocument{
		File:     file,
		MimeType: mimeType,
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeFilename{FileName: filepath.Base(path)},
		},
	}
}

// photoSizeLimit is the largest file Telegram accepts as an uploaded
// photo. Past it the uploader switches to inputFileBig, which
// inputMediaUploadedPhoto rejects — so check up front and fail with an
// actionable message instead of an opaque upstream error.
const photoSizeLimit = 10 << 20 // 10 MiB

// SendPhotoMessage uploads a local image and sends it as a photo, so it
// renders inline in the chat rather than as a file attachment.
// Images above photoSizeLimit must be sent with SendFileMessage instead.
//
// placeholderID names the local echo to swap, as in SendFileMessage.
func (c *Client) SendPhotoMessage(chatID int64, path, caption string, replyToMessageID int64, placeholderID int64) (*Message, error) {
	msg, _, err := c.SendPhotoMessageWithMentions(chatID, path, caption, nil, replyToMessageID, placeholderID)
	return msg, err
}

// SendPhotoMessageWithMentions is SendPhotoMessage for a caption that
// mentions users without a username (see MentionSpan). Dropped mentions
// are counted as in SendTextMessageWithMentions.
func (c *Client) SendPhotoMessageWithMentions(chatID int64, path, caption string, mentions []MentionSpan, replyToMessageID int64, placeholderID int64) (*Message, int, error) {
	ctx, cancel := transferCtx()
	defer cancel()

	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("send photo: %w", err)
	}
	// Checked against the file on disk, and before the cached upload is
	// consumed: the limit is inputMediaUploadedPhoto's, not the upload's,
	// so an eagerly uploaded image is just as ineligible — and the entry
	// has to survive, because the way out of this error is to send the
	// same already-uploaded file as a document instead.
	if info.Size() > photoSizeLimit {
		return nil, 0, fmt.Errorf(
			"send photo: image too large to send as photo (%.1f MB, limit 10 MB) — send it as a file instead",
			float64(info.Size())/(1<<20))
	}

	peer, inputFile, err := c.uploadForSend(ctx, chatID, path)
	if err != nil {
		return nil, 0, fmt.Errorf("send photo: %w", err)
	}

	media := &tg.InputMediaUploadedPhoto{File: inputFile}

	msg, dropped, err := c.sendUploadedMedia(ctx, peer, media, caption, mentions, replyToMessageID, placeholderID)
	if err != nil {
		return nil, 0, fmt.Errorf("send photo: %w", err)
	}
	return msg, dropped, nil
}

// uploadForSend resolves the target peer and uploads path to Telegram.
func (c *Client) uploadForSend(ctx context.Context, chatID int64, path string) (tg.InputPeerClass, tg.InputFileClass, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, fmt.Errorf("%q is a directory", path)
	}

	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, nil, err
	}

	// The file may already be up: StartUpload puts it on Telegram's
	// servers when it is attached, which is normally seconds before this.
	// A cached failure is not one — the entry is dropped and the upload
	// retried here, synchronously, because the reason it failed (a dropped
	// link, most often) is usually gone by now.
	if file, cached, err := c.uploads.await(ctx, path); cached && err == nil {
		return peer, file, nil
	}

	inputFile, err := c.uploadFile(ctx, path, c.uploads.nextGeneration())
	if err != nil {
		return nil, nil, fmt.Errorf("upload %q: %w", path, err)
	}
	return peer, inputFile, nil
}

// sendUploadedMedia sends already-uploaded media to peer and publishes the
// resulting message to the update stream. It also returns how many of the
// caption's mentions were dropped.
func (c *Client) sendUploadedMedia(ctx context.Context, peer tg.InputPeerClass, media tg.InputMediaClass, caption string, mentions []MentionSpan, replyToMessageID int64, placeholderID int64) (*Message, int, error) {
	body, entities, dropped := c.formatOutgoingWithMentions(ctx, caption, mentions)
	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		Message:  body,
		Entities: entities,
		RandomID: rand.Int63(),
	}
	if replyToMessageID != 0 {
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: int(replyToMessageID)}
	}

	updates, err := c.api.MessagesSendMedia(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	msg := messageFromUpdates(c, updates)
	if msg == nil {
		return nil, 0, fmt.Errorf("no message in response")
	}
	c.send(MessageSendSucceededMsg{Message: msg, OldMessageId: placeholderID})
	return msg, dropped, nil
}
