package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/td/session"
	gotd "github.com/gotd/td/telegram"
	gotdauth "github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/imtaqin/telegram-cli/internal/config"
)

// ErrQRPasswordPromptRequired is returned when QR login reaches 2FA but no
// password prompt was configured.
var ErrQRPasswordPromptRequired = errors.New("QR login requires a 2FA password prompt")

// QRLoginToken is a short-lived Telegram login token to render as a QR code.
type QRLoginToken struct {
	URL       string
	ExpiresAt time.Time
}

// QRLoginOptions supplies the interactive parts of QR authentication.
type QRLoginOptions struct {
	// ShowQRCode is called whenever Telegram issues or refreshes a QR token.
	ShowQRCode func(context.Context, QRLoginToken) error

	// PasswordPrompt is called if the account requires 2FA. retry is true
	// after an empty or invalid password. The returned byte slice is consumed
	// and wiped before the function returns.
	PasswordPrompt func(ctx context.Context, retry bool) ([]byte, error)
}

// LoginWithQR authorizes cfg.Storage.SessionFile by scanning a QR code in an
// already authorized Telegram app. Expired QR tokens are refreshed by gotd.
func LoginWithQR(ctx context.Context, cfg *config.Config, opts QRLoginOptions) (*User, error) {
	if opts.ShowQRCode == nil {
		return nil, errors.New("QR login requires a QR code callback")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Storage.SessionFile), 0o755); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}

	dispatcher := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(dispatcher)
	client := gotd.NewClient(int(cfg.Telegram.APIID), cfg.Telegram.APIHash, gotd.Options{
		SessionStorage: &session.FileStorage{Path: cfg.Storage.SessionFile},
		UpdateHandler:  dispatcher,
		Device: gotd.DeviceConfig{
			DeviceModel:    "Telegram CLI",
			SystemVersion:  "1.0.0",
			AppVersion:     "0.1.0",
			SystemLangCode: "en",
			LangCode:       "en",
		},
	})

	var result *User
	err := client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("check authorization status: %w", err)
		}
		if status.Authorized {
			if status.User == nil {
				return errors.New("authorized Telegram session has no user")
			}
			result = userFromTG(status.User)
			return nil
		}

		err = finishQRAuthentication(
			ctx,
			func(ctx context.Context) error {
				_, err := client.QR().Auth(ctx, loggedIn, func(ctx context.Context, token qrlogin.Token) error {
					return opts.ShowQRCode(ctx, QRLoginToken{
						URL:       token.URL(),
						ExpiresAt: token.Expires(),
					})
				})
				return err
			},
			func(ctx context.Context) error {
				return completeQRPassword(ctx, client.Auth(), opts.PasswordPrompt)
			},
		)
		if err != nil {
			return fmt.Errorf("QR authentication: %w", err)
		}

		status, err = client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("verify authorization status: %w", err)
		}
		if !status.Authorized || status.User == nil {
			return errors.New("Telegram did not authorize the QR session")
		}
		result = userFromTG(status.User)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("QR login completed without an authorized user")
	}
	return result, nil
}

func finishQRAuthentication(
	ctx context.Context,
	qrAuth func(context.Context) error,
	passwordAuth func(context.Context) error,
) error {
	err := qrAuth(ctx)
	if err == nil {
		return nil
	}
	if !tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		return err
	}
	return passwordAuth(ctx)
}

type qrPasswordClient interface {
	PasswordWith(context.Context, gotdauth.PasswordHashFunc) (*tg.AuthAuthorization, error)
}

func completeQRPassword(
	ctx context.Context,
	client qrPasswordClient,
	prompt func(context.Context, bool) ([]byte, error),
) error {
	if prompt == nil {
		return ErrQRPasswordPromptRequired
	}

	retry := false
	for {
		secret, err := prompt(ctx, retry)
		if err != nil {
			return fmt.Errorf("read 2FA password: %w", err)
		}
		if len(secret) == 0 {
			retry = true
			continue
		}

		_, err = client.PasswordWith(ctx, gotdauth.PasswordHashFor(secret))
		wipeBytes(secret)
		if err == nil {
			return nil
		}
		if errors.Is(err, gotdauth.ErrPasswordInvalid) {
			retry = true
			continue
		}
		return fmt.Errorf("verify 2FA password: %w", err)
	}
}

func wipeBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
