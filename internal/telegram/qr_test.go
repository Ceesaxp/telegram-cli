package telegram

import (
	"context"
	"errors"
	"fmt"
	"testing"

	gotdauth "github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func TestFinishQRAuthenticationFallsBackTo2FA(t *testing.T) {
	passwordCalled := false
	err := finishQRAuthentication(
		context.Background(),
		func(context.Context) error {
			return fmt.Errorf("export login token: %w", tgerr.New(401, "SESSION_PASSWORD_NEEDED"))
		},
		func(context.Context) error {
			passwordCalled = true
			return nil
		},
	)

	if err != nil {
		t.Fatalf("finish QR authentication: %v", err)
	}
	if !passwordCalled {
		t.Fatal("expected the 2FA password flow to run")
	}
}

func TestFinishQRAuthenticationReturnsOtherErrors(t *testing.T) {
	want := errors.New("QR export failed")
	passwordCalled := false
	err := finishQRAuthentication(
		context.Background(),
		func(context.Context) error { return want },
		func(context.Context) error {
			passwordCalled = true
			return nil
		},
	)

	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
	if passwordCalled {
		t.Fatal("did not expect the 2FA password flow to run")
	}
}

func TestCompleteQRPasswordRetriesAndWipesSecrets(t *testing.T) {
	first := []byte("wrong password")
	second := []byte("correct password")
	promptCalls := 0
	client := &fakeQRPasswordClient{errors: []error{gotdauth.ErrPasswordInvalid, nil}}

	err := completeQRPassword(context.Background(), client, func(_ context.Context, retry bool) ([]byte, error) {
		if retry != (promptCalls > 0) {
			t.Fatalf("unexpected retry value %t on prompt call %d", retry, promptCalls+1)
		}
		promptCalls++
		if promptCalls == 1 {
			return first, nil
		}
		return second, nil
	})

	if err != nil {
		t.Fatalf("complete QR password: %v", err)
	}
	if promptCalls != 2 {
		t.Fatalf("expected 2 password prompts, got %d", promptCalls)
	}
	assertZeroed(t, first)
	assertZeroed(t, second)
}

func TestCompleteQRPasswordWipesSecretWhenPromptFails(t *testing.T) {
	secret := []byte("partial password")
	wantErr := errors.New("interrupted read")
	client := &fakeQRPasswordClient{}

	err := completeQRPassword(context.Background(), client, func(context.Context, bool) ([]byte, error) {
		return secret, wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("expected prompt error %v, got %v", wantErr, err)
	}
	if client.calls != 0 {
		t.Fatalf("expected no password verification calls, got %d", client.calls)
	}
	assertZeroed(t, secret)
}

func assertZeroed(t *testing.T, secret []byte) {
	t.Helper()
	for i, b := range secret {
		if b != 0 {
			t.Fatalf("secret byte %d was not wiped", i)
		}
	}
}

type fakeQRPasswordClient struct {
	errors []error
	calls  int
}

func (f *fakeQRPasswordClient) PasswordWith(context.Context, gotdauth.PasswordHashFunc) (*tg.AuthAuthorization, error) {
	err := f.errors[f.calls]
	f.calls++
	if err != nil {
		return nil, err
	}
	return &tg.AuthAuthorization{}, nil
}
