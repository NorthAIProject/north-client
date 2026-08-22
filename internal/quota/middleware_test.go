package quota_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/users"
)

// guarded builds the handler under test plus a counter that records whether the
// protected work ever ran. A limiter that refuses but still does the work is
// worse than no limiter, because it costs the same and reports success.
func guarded(t *testing.T, limit quota.Limit, counter quota.Counter) (http.Handler, users.User, *int) {
	t.Helper()

	user := users.User{ID: uuid.New()}
	svc := quota.NewService(counter, quota.NewLimits(map[quota.Action]quota.Limit{
		quota.ReportGenerate: limit,
	}, nil), func(context.Context) (quota.Identity, bool) {
		return quota.Identity{UserID: user.ID, Tier: string(user.Tier)}, true
	})

	reached := 0
	handler := svc.Guard(quota.ReportGenerate)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	}))

	return handler, user, &reached
}

func request(t *testing.T, h http.Handler, _ users.User) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/app/reports/generate", nil)
	req.Header.Set("HX-Request", "true")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAGuardedRouteRunsWhileTheBudgetHolds(t *testing.T) {
	h, user, reached := guarded(t, quota.Limit{PerWindow: 2, Window: time.Hour}, &fakeCounter{})

	if code := request(t, h, user).Code; code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if *reached != 1 {
		t.Errorf("the protected handler ran %d times, want 1", *reached)
	}
}

func TestAGuardedRouteRefusesWithoutDoingTheWork(t *testing.T) {
	h, user, reached := guarded(t, quota.Limit{PerWindow: 1, Window: time.Hour}, &fakeCounter{})

	request(t, h, user)
	rec := request(t, h, user)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if *reached != 1 {
		t.Errorf("the protected handler ran %d times, want 1 — the refused request did the work anyway", *reached)
	}
}

// NOR-41's acceptance criterion, literally: exceeding a limit is a friendly
// error, not a 500 and not an empty body the browser renders as a blank panel.
func TestARefusalRendersSomethingAPersonCanRead(t *testing.T) {
	h, user, _ := guarded(t, quota.Limit{PerWindow: 1, Window: time.Hour}, &fakeCounter{})

	request(t, h, user)
	rec := request(t, h, user)

	body := rec.Body.String()
	if strings.TrimSpace(body) == "" {
		t.Fatal("a refusal rendered an empty body; the user sees a blank panel")
	}
	if !strings.Contains(body, "role=\"alert\"") {
		t.Errorf("the refusal did not render the alert component; body = %q", body)
	}
}

func TestARefusalTellsTheClientWhenToRetry(t *testing.T) {
	h, user, _ := guarded(t, quota.Limit{PerWindow: 1, Window: time.Hour}, &fakeCounter{})

	request(t, h, user)
	rec := request(t, h, user)

	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 with no Retry-After leaves a well-behaved client guessing")
	}
}

// The counter being down must not be what stops a person generating a report.
func TestAGuardedRouteStillRunsWhenTheCounterIsDown(t *testing.T) {
	h, user, reached := guarded(t, quota.Limit{PerWindow: 1, Window: time.Hour}, brokenCounter{})

	for range 3 {
		if code := request(t, h, user).Code; code != http.StatusOK {
			t.Fatalf("status = %d, want 200 — a counter outage became an application outage", code)
		}
	}
	if *reached != 3 {
		t.Errorf("the protected handler ran %d times, want 3", *reached)
	}
}

// fakeCounter counts in memory, so the middleware tests need no database.
type fakeCounter struct {
	used  int
	start time.Time
}

func (c *fakeCounter) Consume(_ context.Context, _ uuid.UUID, _ quota.Action, _ time.Duration) (quota.Count, error) {
	if c.start.IsZero() {
		c.start = time.Now()
	}
	c.used++
	return quota.Count{Used: c.used, WindowStart: c.start}, nil
}

func (c *fakeCounter) Sweep(context.Context, time.Time) error { return nil }
