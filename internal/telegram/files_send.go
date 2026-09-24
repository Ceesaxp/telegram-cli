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
	"sync/atomic"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// SendRoots is the set of directories a remote caller — the MCP send_file
// tool, POST /api/send-file — may send a file from. The servers make one
// when they start, from [config.Config.PrepareSendRoots], and open every
// file through it; the TUI has none, because there the person choosing
// the file is the person running the process.
//
// Every root is opened once, when the server starts, and held open until
// it stops. Opening a root by name on each send would follow whatever the
// name pointed at by then: with nested roots A and A/B, anyone who can
// write to A swaps A/B for a link to ~/.ssh, and a root missing at the
// start can be created later as a link to anywhere. A held root is the
// directory it was, wherever its name points afterwards.
//
// A nil SendRoots has no roots and refuses every path.
type SendRoots struct {
	roots  []*os.Root  // held open, one per usable root
	dirs   []string    // those roots as configured, for the refusal
	names  []rootName  // every name a path may be matched under, in order
	closed atomic.Bool // set by Close, which a send may race at shutdown
}

// rootName is one absolute name a held root answers to.
type rootName struct {
	name string
	root *os.Root
}

// OpenSendRoots opens dirs, in the order they are to be searched, and
// holds them open until Close. Blank entries are skipped. A directory that
// cannot be opened — above all one that does not exist yet — is skipped
// too, for as long as the SendRoots lives, and reported in errs so the
// caller can say so.
//
// Each root answers to its name as written, made absolute, and to where it
// really was when it was opened, so a root under /tmp on macOS also
// matches the same path under /private/tmp. The names only choose a root;
// what is opened is always inside the directory held here.
func OpenSendRoots(dirs ...string) (roots *SendRoots, errs []error) {
	roots = &SendRoots{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("send root %s: %w", dir, err))
			continue
		}
		root, err := os.OpenRoot(abs)
		if err != nil {
			errs = append(errs, fmt.Errorf("send root %s: %w", dir, cause(err)))
			continue
		}
		roots.roots = append(roots.roots, root)
		roots.dirs = append(roots.dirs, dir)
		roots.names = append(roots.names, rootName{abs, root})
		if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != abs {
			roots.names = append(roots.names, rootName{resolved, root})
		}
	}
	return roots, errs
}

// Close releases the roots. Files already opened through them stay open;
// Open refuses every path from now on, saying the roots are closed.
// Closing twice does nothing.
func (r *SendRoots) Close() error {
	if r == nil || r.closed.Swap(true) {
		return nil
	}
	var errs []error
	for _, root := range r.roots {
		errs = append(errs, root.Close())
	}
	return errors.Join(errs...)
}

// Open opens path for a remote caller's send if it names a regular file
// inside one of the roots. The send then reads that descriptor (see
// [Client.SendOpenedFileMessage]) and never goes back to the path.
//
// That is the point of it. Checking a path and then opening it by name
// leaves a gap in which anyone who can write to a root swaps the checked
// file for a symlink to one outside, and the upload reads that instead.
// Here the check is the open: path is made absolute and clean, taken
// relative to each root in turn (its directory may be resolved to choose
// the root, see namesFor, but the open never relies on that), and opened
// through an [os.Root], which follows a link only while it stays inside.
// Whatever the name points at afterwards, what is sent is what was opened.
//
// With no usable root everything is refused. A link inside a root must be
// relative and stay inside: os.Root refuses an absolute link even when it
// names a file in the root.
func (r *SendRoots) Open(path string) (*os.File, error) {
	if r == nil {
		r = &SendRoots{}
	}
	if r.closed.Load() {
		return nil, fmt.Errorf("send file: %w", errRootsClosed)
	}
	if strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("send file: %q: %w", path, errInvalidPath)
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("send file: %w", err)
	}
	// found is why a root that holds the path could not send it. It
	// outranks "outside": the path was inside, and saying otherwise would
	// send the caller looking for a typo that is not there.
	var found error
	for _, candidate := range namesFor(abs) {
		for _, n := range r.names {
			rel, ok := within(n.name, candidate)
			if !ok {
				continue
			}
			f, err := openRegular(n.root, rel)
			if err == nil {
				return f, nil
			}
			if why := whyInside(err); found == nil && why != nil {
				found = fmt.Errorf("send file: %q: %w", path, why)
			}
		}
	}
	if found != nil {
		return nil, found
	}
	// Name the roots. The caller here is an authenticated operator or the
	// agent they configured, the set is already logged at startup and
	// documented, and "outside the allowed directories" with no list is a
	// dead end — the reader cannot tell a typo from a policy. Only the
	// roots actually held are named: one that could not be opened at the
	// start was reported then, and is not searched.
	return nil, fmt.Errorf("send file: path %q is outside the allowed directories (%s)",
		path, strings.Join(r.dirs, ", "))
}

// errNotRegular refuses what is not a plain file: a directory, a fifo, a
// device. There is nothing in one to send, and reading some of them has
// side effects or never ends.
var errNotRegular = errors.New("not a regular file")

// errInvalidPath refuses a path no file can have: one with a NUL in it.
var errInvalidPath = errors.New("invalid path")

// errRootsClosed refuses a send that arrives once the roots are closed,
// at shutdown, whatever its path: none of them is searched any more.
var errRootsClosed = errors.New("send roots closed")

// openRegular opens name inside root, and only if it is a regular file.
// An [os.Root] refuses a ".." or a symlink that points outside it,
// checking each link as it follows it on the descriptors it opens; the
// type is then asked of the descriptor, not of the path, which may name
// something else by now.
//
// The file is not tied to the root: it stays open when the root is
// closed, which is what [os.OpenInRoot] relies on too.
func openRegular(root *os.Root, name string) (*os.File, error) {
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

// whyInside is why a path inside a root cannot be sent, given the error
// opening it: the file is not regular, is missing or unreadable, or the
// way to it breaks inside the root (see whyInsideOS). It is nil for
// anything else — above all os.Root's refusal of a path that leaves it,
// which has no exported error of its own — and the path then reads as
// outside.
func whyInside(err error) error {
	err = cause(err)
	if errors.Is(err, fs.ErrClosed) {
		// A send that was already searching when Close ran at shutdown.
		return errRootsClosed
	}
	if errors.Is(err, errNotRegular) ||
		errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, fs.ErrPermission) {
		return err
	}
	return whyInsideOS(err)
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

// namesFor is abs as given and, when its directory resolves somewhere
// else, the same name in the resolved directory: a caller's /var/... names
// what a root written /private/var/... holds on macOS.
//
// This only chooses the root and the path within it. Resolving the parent
// here is not the race it would be for the open itself, because the open
// still happens inside a held root, which refuses anything that leaves it
// whatever the parent resolved to; a parent swapped in between can at most
// pick a different file inside a root. The file's own name is never
// resolved, so a final link is still os.Root's to judge. A path that is not
// on a local volume is never resolved at all (see resolvableVolume): this
// runs before any root is matched, on whatever path the caller sent.
func namesFor(abs string) []string {
	names := []string{abs}
	if !resolvableVolume(filepath.VolumeName(abs)) {
		return names
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return names
	}
	if resolved := filepath.Join(dir, filepath.Base(abs)); resolved != abs {
		names = append(names, resolved)
	}
	return names
}

// resolvableVolume reports whether a caller's path on volume, as
// [filepath.VolumeName] gives it, may be resolved before a root is
// matched. Resolving touches the filesystem the path names, and on Windows
// a path on a network share — \\host\share, //host/share, \\?\UNC\...,
// \\.\UNC\..., or \??\UNC\..., which Windows passes to the NT namespace as
// it is — makes that an SMB connection to a host the caller chose, which
// hands it the user's NTLM hash. So only a local volume is resolved: none,
// which is every path on unix, or a bare drive letter. Anything else is
// matched as written, which a root on a share still does; device paths
// such as \\?\C:\ lose only the match through a linked directory.
//
// A string test rather than a question put to the OS, so that it can be
// tested with Windows volumes on any platform.
func resolvableVolume(volume string) bool {
	return volume == "" || (len(volume) == 2 && volume[1] == ':')
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

// SendFileMessage uploads a local file and sends it as a document,
// optionally with a caption and as a reply.
//
// placeholderID is the local echo the caller already drew for this send, so
// the success message can name the row to swap; 0 when there is none.
//
// chatID may name a forum topic, as it may for [Client.SendTextMessage],
// and every send below goes out through the same target and reply header.
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

	target, inputFile, err := c.uploadForSend(ctx, chatID, path)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: %w", err)
	}

	msg, dropped, err := c.sendUploadedMedia(ctx, target, documentMedia(inputFile, path), caption, mentions, replyToMessageID, placeholderID)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: %w", err)
	}
	return msg, dropped, nil
}

// SendOpenedFileMessage is SendFileMessage for a file that is already open,
// normally by [SendRoots.Open]: it uploads from f and never opens
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
// which for [SendRoots.Open] is the path the caller asked for — a
// link's own name, not its target's.
func (c *Client) SendOpenedFileMessageWithMentions(chatID int64, f *os.File, caption string, mentions []MentionSpan, replyToMessageID int64, placeholderID int64) (*Message, int, error) {
	defer f.Close()
	ctx, cancel := transferCtx()
	defer cancel()

	target, err := c.targetFor(ctx, chatID)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: %w", err)
	}
	name := filepath.Base(f.Name())
	inputFile, err := c.uploadOpened(ctx, f, name)
	if err != nil {
		return nil, 0, fmt.Errorf("send file: upload %q: %w", name, err)
	}

	msg, dropped, err := c.sendUploadedMedia(ctx, target, documentMedia(inputFile, name), caption, mentions, replyToMessageID, placeholderID)
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

	target, inputFile, err := c.uploadForSend(ctx, chatID, path)
	if err != nil {
		return nil, 0, fmt.Errorf("send photo: %w", err)
	}

	media := &tg.InputMediaUploadedPhoto{File: inputFile}

	msg, dropped, err := c.sendUploadedMedia(ctx, target, media, caption, mentions, replyToMessageID, placeholderID)
	if err != nil {
		return nil, 0, fmt.Errorf("send photo: %w", err)
	}
	return msg, dropped, nil
}

// uploadForSend resolves where the send is going and uploads path to
// Telegram. The target carries the topic when the chat ID names one, which
// the send itself needs and the upload does not.
func (c *Client) uploadForSend(ctx context.Context, chatID int64, path string) (chatTarget, tg.InputFileClass, error) {
	info, err := os.Stat(path)
	if err != nil {
		return chatTarget{}, nil, err
	}
	if info.IsDir() {
		return chatTarget{}, nil, fmt.Errorf("%q is a directory", path)
	}

	target, err := c.targetFor(ctx, chatID)
	if err != nil {
		return chatTarget{}, nil, err
	}

	// The file may already be up: StartUpload puts it on Telegram's
	// servers when it is attached, which is normally seconds before this.
	// A cached failure is not one — the entry is dropped and the upload
	// retried here, synchronously, because the reason it failed (a dropped
	// link, most often) is usually gone by now.
	if file, cached, err := c.uploads.await(ctx, path); cached && err == nil {
		return target, file, nil
	}

	inputFile, err := c.uploadFile(ctx, path, c.uploads.nextGeneration())
	if err != nil {
		return chatTarget{}, nil, fmt.Errorf("upload %q: %w", path, err)
	}
	return target, inputFile, nil
}

// sendUploadedMedia sends already-uploaded media to the target and
// publishes the resulting message to the update stream. It also returns how
// many of the caption's mentions were dropped.
func (c *Client) sendUploadedMedia(ctx context.Context, target chatTarget, media tg.InputMediaClass, caption string, mentions []MentionSpan, replyToMessageID int64, placeholderID int64) (*Message, int, error) {
	body, entities, dropped := c.formatOutgoingWithMentions(ctx, caption, mentions)
	req := &tg.MessagesSendMediaRequest{
		Peer:     target.peer,
		Media:    media,
		Message:  body,
		Entities: entities,
		RandomID: rand.Int63(),
	}
	req.ReplyTo = replyHeaderFor(target.topicID, replyToMessageID)

	updates, err := c.api.MessagesSendMedia(ctx, req)
	if err != nil {
		return nil, 0, topicSendError(err)
	}

	msg := messageFromUpdates(c, updates)
	if msg == nil {
		return nil, 0, fmt.Errorf("no message in response")
	}
	c.publishSent(target, msg, placeholderID)
	return msg, dropped, nil
}
