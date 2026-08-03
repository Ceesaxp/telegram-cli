package auth

import (
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

func TestSubmitPhoneWaitsForCodeRequestConfirmation(t *testing.T) {
	authorizer := telegram.NewTUIAuthorizer(&config.Config{})
	model := New(theme.DarkTheme(), authorizer)
	model.input.Value = "+15551234567"

	got, _ := model.submit()

	if got.step != StepLoading {
		t.Fatalf("expected phone submission to wait in loading step, got %v", got.step)
	}
	if got.hint != "Requesting verification code..." {
		t.Fatalf("expected pending request hint, got %q", got.hint)
	}
	if view := got.View(); !strings.Contains(view, "Requesting verification code...") {
		t.Fatalf("expected pending request hint to be rendered, got %q", view)
	}
}
