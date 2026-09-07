package messaging

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The consent gate. Asking permission in a language somebody did not choose is
// the one place where falling back to English is not merely untidy — this is
// the message that stands between the coach and a write.
func TestTheApprovalPromptSpeaksTheAccountLanguage(t *testing.T) {
	pending := coach.PendingCall{Calls: []ai.ToolCall{{Name: "log_check_in"}}}

	cases := map[users.Locale][3]string{
		users.LocaleEN:   {"Before I do this, can you confirm?", "Yes, do it", "No"},
		users.LocalePTPT: {"Antes de fazer isto, confirmas?", "Sim, faz", "Não"},
		users.LocalePTBR: {"Antes de fazer isso, você confirma?", "Sim, pode fazer", "Não"},
		users.LocaleES:   {"Antes de hacer esto, ¿me lo confirmas?", "Sí, hazlo", "No"},
	}

	for locale, want := range cases {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		msg := confirmationMessage(ctx, pending, "")

		if !strings.Contains(msg.Text, want[0]) {
			t.Errorf("%s: prompt = %q, want it to contain %q", locale, msg.Text, want[0])
		}
		if len(msg.Options) != 2 {
			t.Fatalf("%s: expected two answers, got %d", locale, len(msg.Options))
		}
		if msg.Options[0].Label != want[1] {
			t.Errorf("%s: approve label = %q, want %q", locale, msg.Options[0].Label, want[1])
		}
		if msg.Options[1].Label != want[2] {
			t.Errorf("%s: decline label = %q, want %q", locale, msg.Options[1].Label, want[2])
		}

		// The values the adapter matches on are protocol, not copy: translating
		// them would mean a tapped button no longer resolves the pending call.
		if msg.Options[0].Value != AnswerApprove || msg.Options[1].Value != AnswerDecline {
			t.Errorf("%s: the answer values were translated, so a tap would not resolve", locale)
		}
	}
}

// A refusal is a bad moment to change language on somebody.
func TestQuotaRefusalsAreTranslated(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), string(users.LocalePTPT))

	// Each branch of the wait, because each is its own catalogue string.
	cases := map[time.Duration]string{
		30 * time.Second: "menos de um minuto",
		20 * time.Minute: "20 minutos",
		3 * time.Hour:    "cerca de uma hora",
	}
	for after, want := range cases {
		got := quotaMessage(ctx, quota.Decision{RetryAfter: after})
		if !strings.Contains(got, want) {
			t.Errorf("wait %v: message = %q, want it to contain %q", after, got, want)
		}
		if strings.Contains(got, "coach message limit") {
			t.Errorf("wait %v: still English", after)
		}
	}
}
