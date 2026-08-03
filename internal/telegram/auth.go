package telegram

import (
	"context"
	"errors"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	"github.com/imtaqin/telegram-cli/internal/config"
)

// AuthState is the authorization state reported to the TUI.
type AuthState int

const (
	AuthStateWaitPhone AuthState = iota
	AuthStateWaitCode
	AuthStateWaitPassword
	AuthStateReady
	AuthStateClosed
)

// AuthStateCallback is called when the auth state changes.
// Used to notify the TUI about state transitions.
type AuthStateCallback func(AuthState, string)

// ErrLoginRequired is returned by the authorizer in non-interactive mode
// when the session is not authorized yet.
var ErrLoginRequired = errors.New("login required: run 'telegram-mcp login' first")

// TUIAuthorizer implements gotd's auth.UserAuthenticator on top of
// the channel-based flow used by the TUI.
type TUIAuthorizer struct {
	phoneCh    chan string
	codeCh     chan string
	passwordCh chan string
	phone      string
	onState    AuthStateCallback
	onError    func(error)

	// NonInteractive makes Phone/Code/Password fail immediately with
	// ErrLoginRequired instead of waiting for user input (headless mode).
	NonInteractive bool

	// hintFunc fetches the 2FA password hint; set by the client once
	// its API handle exists (gotd's Password() receives no client).
	hintFunc func(ctx context.Context) string
}

func NewTUIAuthorizer(cfg *config.Config) *TUIAuthorizer {
	return &TUIAuthorizer{
		phoneCh:    make(chan string, 1),
		codeCh:     make(chan string, 1),
		passwordCh: make(chan string, 1),
		phone:      cfg.Telegram.Phone,
	}
}

// SetStateCallback sets the callback for auth state changes.
func (a *TUIAuthorizer) SetStateCallback(cb AuthStateCallback) {
	a.onState = cb
}

// SetErrorCallback sets the callback for fatal auth errors (shown in the TUI).
func (a *TUIAuthorizer) SetErrorCallback(cb func(error)) {
	a.onError = cb
}

func (a *TUIAuthorizer) notifyState(state AuthState, hint string) {
	if a.onState != nil {
		a.onState(state, hint)
	}
}

func (a *TUIAuthorizer) notifyError(err error) {
	if a.onError != nil {
		a.onError(err)
	}
}

func (a *TUIAuthorizer) SubmitPhone(phone string) {
	a.phoneCh <- phone
}

func (a *TUIAuthorizer) SubmitCode(code string) {
	a.codeCh <- code
}

func (a *TUIAuthorizer) SubmitPassword(password string) {
	a.passwordCh <- password
}

// Phone implements auth.UserAuthenticator.
// The config-provided phone is consumed once: if the flow fails and
// retries, the TUI is asked instead of reusing a possibly wrong number.
func (a *TUIAuthorizer) Phone(ctx context.Context) (string, error) {
	if a.NonInteractive {
		return "", ErrLoginRequired
	}
	if a.phone != "" {
		phone := a.phone
		a.phone = ""
		return phone, nil
	}
	a.notifyState(AuthStateWaitPhone, "")
	select {
	case phone := <-a.phoneCh:
		return phone, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Code implements auth.CodeAuthenticator.
func (a *TUIAuthorizer) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
	if a.NonInteractive {
		return "", ErrLoginRequired
	}
	a.notifyState(AuthStateWaitCode, "")
	select {
	case code := <-a.codeCh:
		return code, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Password implements auth.UserAuthenticator.
// The 2FA hint is fetched via account.getPassword through hintFunc,
// which the client wires up before the auth flow starts.
func (a *TUIAuthorizer) Password(ctx context.Context) (string, error) {
	if a.NonInteractive {
		return "", ErrLoginRequired
	}
	hint := ""
	if a.hintFunc != nil {
		hint = a.hintFunc(ctx)
	}
	a.notifyState(AuthStateWaitPassword, hint)
	select {
	case password := <-a.passwordCh:
		return password, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// SignUp implements auth.UserAuthenticator.
func (a *TUIAuthorizer) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("signup not supported")
}

// AcceptTermsOfService implements auth.UserAuthenticator.
func (a *TUIAuthorizer) AcceptTermsOfService(ctx context.Context, _ tg.HelpTermsOfService) error {
	return errors.New("signup not supported")
}

func (a *TUIAuthorizer) Close() {
	// Don't close channels — they may still be in use by the TUI.
}
