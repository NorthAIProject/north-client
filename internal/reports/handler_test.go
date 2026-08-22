package reports_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The auth context key is unexported on purpose — "any other package could
// otherwise forge an authenticated request" — so these tests sign in the way a
// browser does: a real session row, a real cookie, the real middleware. That
// also means the tests cover the mounting, not only the handler bodies.
type harness struct {
	router  http.Handler
	svc     *reports.Service
	user    users.User
	cookies []*http.Cookie
}

func newHarness(t *testing.T) harness {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "reader@example.com",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Reader",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	svc := reports.NewService(reports.Options{
		Repository: reports.NewRepository(pool),
		Users:      userSvc,
		Queue:      &stubQueue{},
		Client: &fake.Client{Responses: []fake.Response{{
			Text: "# Week\n\nSomething happened.\n",
		}}},
	})

	sessions := auth.NewSessionStore(pool, time.Hour)
	mw := auth.NewMiddleware(sessions, false)

	r := chi.NewRouter()
	r.Use(mw.LoadUser)
	r.Group(func(r chi.Router) {
		r.Use(mw.RequireAuth)
		// A real quota service, wired the way main.go wires it, so these tests
		// also prove the guard leaves the ordinary path alone. The budget is
		// far above anything a test spends.
		quotas := quota.NewService(
			quota.NewRepository(pool),
			quota.NewLimits(map[quota.Action]quota.Limit{quota.ReportGenerate: {PerWindow: 1000, Window: time.Hour}}, nil),
			func(ctx context.Context) (quota.Identity, bool) {
				u, ok := auth.UserFrom(ctx)
				return quota.Identity{UserID: u.ID, Tier: string(u.Tier)}, ok
			},
		)
		reports.NewHandler(svc, quotas).Routes(r)
	})

	return harness{
		router:  r,
		svc:     svc,
		user:    user,
		cookies: signIn(t, sessions, user),
	}
}

// countingQueue counts enqueues rather than keeping the last one, which is the
// only way to see a job being scheduled more than once.
type countingQueue struct{ n int }

func (q *countingQueue) Enqueue(_ context.Context, kind jobs.Kind, _ any) (jobs.Job, error) {
	q.n++
	return jobs.Job{ID: uuid.New(), Kind: kind}, nil
}

func signIn(t *testing.T, sessions *auth.SessionStore, user users.User) []*http.Cookie {
	t.Helper()
	token, expires, err := sessions.Create(context.Background(), user.ID, auth.Metadata{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return []*http.Cookie{{
		Name:    auth.SessionCookieName,
		Value:   token,
		Path:    "/",
		Expires: expires,
	}}
}

func (h harness) post(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	for _, c := range h.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h harness) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range h.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func TestRegenerateWithAMalformedIDIsNotFound(t *testing.T) {
	h := newHarness(t)

	rec := h.post(t, "/reports/not-a-uuid/regenerate")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestReadingSomebodyElsesReportIsNotFound(t *testing.T) {
	h := newHarness(t)

	// A well-formed id that belongs to nobody is indistinguishable, from the
	// outside, from one belonging to another account — which is the point.
	rec := h.get(t, "/reports/"+uuid.New().String())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRegeneratingAnArchivedReportIsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	report, err := h.svc.RequestGenerate(ctx, h.user.ID, time.Time{})
	if err != nil {
		t.Fatalf("request generate: %v", err)
	}
	if err = h.svc.Archive(ctx, report.ID, h.user.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	rec := h.post(t, "/reports/"+report.ID.String()+"/regenerate")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got, want := rec.Header().Get("Location"),
		"/app/reports/"+report.ID.String()+"?notice=archived"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}

	// The archived row must still be the only report for that week. The bug
	// this pins is a silent second review appearing beside the archived one.
	all, err := h.svc.List(ctx, h.user.ID, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("reports = %d, want 1", len(all))
	}
}

func TestArchiveRedirectsToTheList(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	report, err := h.svc.RequestGenerate(ctx, h.user.ID, time.Time{})
	if err != nil {
		t.Fatalf("request generate: %v", err)
	}

	rec := h.post(t, "/reports/"+report.ID.String()+"/archive")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/app/reports" {
		t.Fatalf("location = %q", got)
	}

	stored, err := h.svc.Get(ctx, report.ID, h.user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !stored.Archived() {
		t.Fatal("report is not archived")
	}
}

func TestRegeneratingInsideTheCooldownSaysSo(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	report, err := h.svc.RequestGenerate(ctx, h.user.ID, time.Time{})
	if err != nil {
		t.Fatalf("request generate: %v", err)
	}
	if err = h.svc.Generate(ctx, report.ID, h.user.ID); err != nil {
		t.Fatalf("generate: %v", err)
	}

	rec := h.post(t, "/reports/"+report.ID.String()+"/regenerate")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got, want := rec.Header().Get("Location"),
		"/app/reports/"+report.ID.String()+"?notice=cooldown"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
}

// A pending report has a job behind it already. Re-posting used to enqueue
// another every time, without limit, because the cooldown only looked at
// GeneratedAt and a report that never finished has none.
func TestRegeneratingAPendingReportDoesNotStackJobs(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "impatient@example.com",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Impatient",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	counter := &countingQueue{}
	svc := reports.NewService(reports.Options{
		Repository: reports.NewRepository(pool),
		Users:      userSvc,
		Queue:      counter,
	})

	for i := 0; i < 4; i++ {
		if _, err = svc.RequestGenerate(ctx, user.ID, time.Time{}); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}

	if counter.n != 1 {
		t.Fatalf("enqueued %d jobs for one pending week, want 1", counter.n)
	}
}
