package ai_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type stubConsent map[uuid.UUID]bool

func (s stubConsent) AllowsAI(_ context.Context, id uuid.UUID) (bool, error) { return s[id], nil }

func TestGatedClient(t *testing.T) {
	yes, no := uuid.New(), uuid.New()
	client := ai.Gated(fake.Text("hello"), stubConsent{yes: true})

	cases := []struct {
		name    string
		ctx     context.Context
		refused bool
	}{
		{"consented", aiattr.WithUser(context.Background(), yes, "coach"), false},
		{"not consented", aiattr.WithUser(context.Background(), no, "coach"), true},
		{"surface with no account holds nobody's data", aiattr.With(context.Background(), aiattr.Attribution{Surface: "daily_post"}), false},
		{"unlabelled fails closed", context.Background(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Generate(tc.ctx, ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
			if got := apperr.Is(err, apperr.ErrAIConsentRequired); got != tc.refused {
				t.Fatalf("refused = %v (err %v), want %v", got, err, tc.refused)
			}
			_, err = client.Chat(tc.ctx, ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
			if got := apperr.Is(err, apperr.ErrAIConsentRequired); got != tc.refused {
				t.Fatalf("chat refused = %v (err %v), want %v", got, err, tc.refused)
			}
		})
	}
}

func TestConsentRefusalDoesNotFailOver(t *testing.T) {
	if ai.Failover(apperr.ErrAIConsentRequired) {
		t.Fatal("a refusal for consent must not walk on to another provider")
	}
}

func TestNilCheckerLeavesClientUngated(t *testing.T) {
	c := fake.Text("hello")
	if ai.Gated(c, nil) != c {
		t.Fatal("nil checker should return the client itself")
	}
}
