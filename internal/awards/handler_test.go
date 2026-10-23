package awards_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// fakePeople resolves handles from a map. A handle not in it is what a
// block or a typo looks like to the real profiles service: not found.
type fakePeople struct {
	byHandle map[string]awards.Person
	handles  map[uuid.UUID]string
}

func (f fakePeople) Person(_ context.Context, viewer users.User, handle string) (awards.Person, error) {
	p, ok := f.byHandle[handle]
	if !ok {
		return awards.Person{}, apperr.ErrNotFound
	}
	p.Self = p.ID == viewer.ID
	p.Shared = p.Shared || p.Self
	return p, nil
}

func (f fakePeople) HandleOf(_ context.Context, id uuid.UUID) (string, error) {
	return f.handles[id], nil
}

type pageEnv struct {
	t     *testing.T
	svc   *awards.Service
	r     chi.Router
	owner users.User
	other users.User
}

func newPageEnv(t *testing.T, ownerShares bool) *pageEnv {
	t.Helper()
	api := newAPIEnv(t, "owner@north.test")
	otherUser, err := users.NewService(users.NewRepository(api.pool())).Register(context.Background(), users.Registration{
		Email: "viewer@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Vic", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	people := fakePeople{
		byHandle: map[string]awards.Person{
			"pat": {ID: api.user.ID, Name: "Pat", Handle: "pat", Shared: ownerShares},
		},
		handles: map[uuid.UUID]string{api.user.ID: "pat"},
	}
	r := chi.NewRouter()
	r.Route("/app", awards.NewHandler(api.svc, people).Routes)
	return &pageEnv{t: t, svc: api.svc, r: r, owner: api.user, other: otherUser}
}

func (e *pageEnv) do(method, path string, as users.User, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.r.ServeHTTP(rec, req.WithContext(auth.ContextWithUser(req.Context(), as)))
	return rec
}

func (e *pageEnv) grant(id string) {
	e.svc.Grant(context.Background(), e.owner.ID, time.Now(), id)
}

func TestUnseenReturns204WhenNone(t *testing.T) {
	e := newPageEnv(t, true)
	rec := e.do(http.MethodGet, "/app/awards/unseen", e.owner, nil)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d, body %q; want an empty 204", rec.Code, rec.Body.String())
	}
}

func TestUnseenIsOneDialogListingEveryBadge(t *testing.T) {
	e := newPageEnv(t, true)
	e.grant("workouts_1")
	e.grant("streak_7")
	rec := e.do(http.MethodGet, "/app/awards/unseen", e.owner, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"First Workout", "7-Day Streak", "You earned 2 medals",
		`data-tui-dialog`, `id="awards-unlock-dialog"`,
		`value="workouts_1"`, `value="streak_7"`, `hx-post="/app/awards/seen"`, "View on profile", "Done",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment lacks %q", want)
		}
	}
	if n := strings.Count(body, "<dialog"); n != 1 {
		t.Errorf("%d dialogs, want one", n)
	}
}

func TestSeenBatchMarksEveryBadge(t *testing.T) {
	e := newPageEnv(t, true)
	e.grant("workouts_1")
	e.grant("streak_7")
	rec := e.do(http.MethodPost, "/app/awards/seen", e.owner, url.Values{"badge": {"workouts_1", "streak_7"}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/app/awards/unseen", e.owner, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("still unseen after seen: %d", rec.Code)
	}
}

func TestViewOnProfileMarksSeenAndGoesToTheProfile(t *testing.T) {
	e := newPageEnv(t, true)
	e.grant("workouts_1")
	rec := e.do(http.MethodPost, "/app/awards/seen", e.owner, url.Values{"badge": {"workouts_1"}, "to": {"profile"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/app/u/pat" {
		t.Fatalf("status %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := e.do(http.MethodGet, "/app/awards/unseen", e.owner, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("still unseen: %d", rec.Code)
	}
}

func TestSeenRejectsAnUnknownBadgeWithoutMarkingTheRest(t *testing.T) {
	e := newPageEnv(t, true)
	e.grant("workouts_1")
	rec := e.do(http.MethodPost, "/app/awards/seen", e.owner, url.Values{"badge": {"workouts_1", "nope"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/app/awards/unseen", e.owner, nil); rec.Code != http.StatusOK {
		t.Fatalf("a rejected request marked a badge: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/app/awards/nope/seen", e.owner, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("single unknown = %d", rec.Code)
	}
}

func TestSeenOneMarksIt(t *testing.T) {
	e := newPageEnv(t, true)
	e.grant("workouts_1")
	if rec := e.do(http.MethodPost, "/app/awards/workouts_1/seen", e.owner, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/app/awards/unseen", e.owner, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("still unseen: %d", rec.Code)
	}
}

func TestOwnerPageShowsEarnedAndLockedWithProgress(t *testing.T) {
	e := newPageEnv(t, true)
	e.grant("workouts_1")
	for _, path := range []string{"/app/u/pat/awards", "/app/awards"} {
		rec := e.do(http.MethodGet, path, e.owner, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
		for _, want := range []string{"First Workout", "10 Workouts", "0/10", "Still to earn"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
	}
}

func TestOtherSeesOnlySharedPublicBadges(t *testing.T) {
	e := newPageEnv(t, true)
	e.grant("workouts_1")
	e.grant("water_week") // a private area: only its owner ever sees it
	rec := e.do(http.MethodGet, "/app/u/pat/awards", e.other, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "First Workout") {
		t.Fatalf("status %d, shared badge missing", rec.Code)
	}
	if strings.Contains(body, "Still to earn") || strings.Contains(body, "10 Workouts") {
		t.Error("another person's page shows locked badges")
	}
	if strings.Contains(body, "A Week of Water") {
		t.Error("a private badge reached another person")
	}
}

func TestOtherSeesNothingWhenAwardsAreNotShared(t *testing.T) {
	e := newPageEnv(t, false)
	e.grant("workouts_1")
	rec := e.do(http.MethodGet, "/app/u/pat/awards", e.other, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "First Workout") {
		t.Fatalf("status %d; the switch is off but a badge shows", rec.Code)
	}
}

func TestUnknownOrBlockedHandleIs404(t *testing.T) {
	e := newPageEnv(t, true)
	if rec := e.do(http.MethodGet, "/app/u/nobody/awards", e.other, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestUnlockFragmentIsTranslatedInEveryLocale(t *testing.T) {
	e := newPageEnv(t, true)
	e.grant("workouts_1")
	for _, tag := range []string{"en", "es", "pt-PT", "pt-BR"} {
		req := httptest.NewRequest(http.MethodGet, "/app/awards/unseen", nil)
		ctx := i18n.WithLocale(auth.ContextWithUser(req.Context(), e.owner), tag)
		rec := httptest.NewRecorder()
		e.r.ServeHTTP(rec, req.WithContext(ctx))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || strings.Contains(body, "awards.") {
			t.Errorf("%s: status %d or an untranslated key in the fragment", tag, rec.Code)
		}
		if want := i18n.Translate(tag, "awards.unlock.done"); !strings.Contains(body, want) {
			t.Errorf("%s: lacks %q", tag, want)
		}
	}
}
