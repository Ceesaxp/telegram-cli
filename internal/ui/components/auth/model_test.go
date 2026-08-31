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
	model := New(theme.DarkRoles(false), authorizer)
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

func TestPasswordInputIsMasked(t *testing.T) {
	authorizer := telegram.NewTUIAuthorizer(&config.Config{})
	model := New(theme.DarkRoles(false), authorizer)
	model.SetSize(80, 24)

	model.SetStep(StepPassword)
	model.input.Value = "secret-pass"
	model.input.Focused = true
	view := model.View()
	if strings.Contains(view, "secret-pass") {
		t.Fatalf("password step View leaked the password: %q", view)
	}
	if !strings.Contains(view, "•") && !strings.Contains(view, "*") {
		t.Fatalf("password step View has no mask glyphs: %q", view)
	}

	model.SetStep(StepPhone)
	model.input.Value = "+15551234567"
	model.input.Focused = true
	view = model.View()
	if !strings.Contains(view, "+15551234567") {
		t.Fatalf("phone step View should show the input, got %q", view)
	}
}
