package insights

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeBadges struct {
	challenge awards.MonthlyChallenge
	calls     int
}

func (f *fakeBadges) Earned(context.Context, uuid.UUID) ([]awards.Earned, error) { return nil, nil }

func (f *fakeBadges) Monthly(context.Context, uuid.UUID, time.Time) (awards.MonthlyChallenge, error) {
	f.calls++
	return f.challenge, nil
}

// awardsCard is the handler wiring: it must read the owner's challenge and
// put its progress on the card.
func TestAwardsCardCarriesTheMonthlyProgressLine(t *testing.T) {
	f := &fakeBadges{challenge: awards.MonthlyChallenge{Key: "2026-10", Metric: awards.MonthlyMetric("workouts"), Target: 8, Progress: 3}}
	h := (&Handler{}).WithAwards(f)
	req := httptest.NewRequest(http.MethodGet, "/app/insights", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{ID: uuid.New()}))

	card := h.awardsCard(req)
	if card == nil {
		t.Fatal("no card")
	}
	var buf bytes.Buffer
	if err := card.Render(req.Context(), &buf); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || !strings.Contains(buf.String(), "3 of 8 workouts") || !strings.Contains(buf.String(), "data-monthly-summary") {
		t.Errorf("calls=%d, card lacks the progress line: %s", f.calls, buf.String())
	}

	f.challenge.Progress, f.challenge.Met = 8, true
	card = h.awardsCard(req)
	buf.Reset()
	if err := card.Render(req.Context(), &buf); err != nil || !strings.Contains(buf.String(), "Challenge met") {
		t.Errorf("met card lacks Challenge met: %v %s", err, buf.String())
	}
}
