package lighterday

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// lowMorning is two weeks of HRV around 52 ms and resting heart rate around
// 56 bpm, then a morning of 38 ms and 61 bpm.
type lowMorning struct{ at time.Time }

func (l lowMorning) Between(_ context.Context, _ uuid.UUID, metric string, _, _ time.Time) ([]health.Stored, error) {
	base, today := 52.0, 38.0
	if metric == "resting_heart_rate" {
		base, today = 56, 61
	}
	var out []health.Stored
	for d := 1; d <= 14; d++ {
		out = append(out, health.Stored{Value: base + float64(d%3-1), StartedAt: l.at.AddDate(0, 0, -d)})
	}
	return append(out, health.Stored{Value: today, StartedAt: l.at}), nil
}

func TestLighterShape(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 7, 6, 30, 0, 0, time.UTC)
	r, err := insights.NewRecoverySource(lowMorning{at: at}, nil).Recovery(context.Background(), users.User{Timezone: "UTC"}, at)
	if err != nil || !r.Low() {
		t.Fatalf("sample recovery = %+v, %v; want low", r.Score, err)
	}
	apitest.AssertGolden(t, "today-lighter.golden.json", project(context.Background(), Today{
		Recovery: r,
		Session:  "Lower body",
		Due:      true,
	}))
}

// The reason is shown verbatim by the app and the web card, so it follows the
// reader's language; the coach keeps reading Why's English.
func TestLighterReasonSpeaksTheReadersLanguage(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 7, 6, 30, 0, 0, time.UTC)
	r, err := insights.NewRecoverySource(lowMorning{at: at}, nil).Recovery(context.Background(), users.User{Timezone: "UTC"}, at)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ locale, want string }{
		{"es", "Variabilidad de la frecuencia cardiaca: por debajo de lo habitual, 38ms el mié 7 oct (normalmente 51ms–53ms)."},
		{"pt-PT", "Variabilidade da frequência cardíaca: abaixo do habitual, 38ms ("},
	}
	for _, tt := range tests {
		ctx := i18n.WithLocale(context.Background(), tt.locale)
		got := project(ctx, Today{Recovery: r, Session: "Lower body", Due: true}).Readiness.Reason
		if !strings.HasPrefix(got, tt.want) {
			t.Errorf("%s reason = %q, want prefix %q", tt.locale, got, tt.want)
		}
	}
	if why := r.Why(); !strings.HasPrefix(why, "Heart rate variability is below your usual") {
		t.Errorf("Why() = %q; the coach's English changed", why)
	}
}
