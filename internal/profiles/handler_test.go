package profiles_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/profiles"
	"github.com/NorthAIProject/north-client/internal/users"
)

// pageClient serves the profile routes under /app, as the app mounts them.
func (w world) pageClient(t *testing.T) func(u users.User, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/app", profiles.NewHandler(w.svc, w.soc).Routes)
	return func(u users.User, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(url.Values{}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(auth.ContextWithUser(req.Context(), u))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
}

func TestProfilePageShowsSharedWorkouts(t *testing.T) {
	w := newWorld(t)
	leo, ana := person(t, w.pool, "leo-pg1@n.test", "Leo"), person(t, w.pool, "ana-pg1@n.test", "Ana")
	w.handle(t, leo, "leo")
	se := w.workout(t, leo, time.Hour)
	do := w.pageClient(t)

	rec := do(ana, http.MethodGet, "/app/u/leo")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"Leo", "@leo", "Strength training", "/app/u/leo/workouts/" + se.ID.String()} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestProfilePageHidesUnsharedWorkouts(t *testing.T) {
	w := newWorld(t)
	leo, ana := person(t, w.pool, "leo-pg2@n.test", "Leo"), person(t, w.pool, "ana-pg2@n.test", "Ana")
	w.handle(t, leo, "leo")
	se := w.workout(t, leo, time.Hour)
	w.share(t, leo, achievements.Sharing{Training: false})
	do := w.pageClient(t)

	rec := do(ana, http.MethodGet, "/app/u/leo")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Workouts aren&#39;t shared.") && !strings.Contains(body, "Workouts aren't shared.") {
		t.Fatalf("status %d, no hidden line", rec.Code)
	}
	if strings.Contains(body, "/workouts/") {
		t.Fatal("workout link on a page that hides workouts")
	}
	if rec := do(ana, http.MethodGet, "/app/u/leo/workouts/"+se.ID.String()); rec.Code != http.StatusNotFound {
		t.Fatalf("workout page = %d", rec.Code)
	}
	if rec := do(ana, http.MethodGet, "/app/u/leo/workouts"); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "/workouts/") {
		t.Fatalf("list page = %d with a link", rec.Code)
	}
}

func TestProfilePageFollowIsInstant(t *testing.T) {
	w := newWorld(t)
	leo, ana := person(t, w.pool, "leo-pg3@n.test", "Leo"), person(t, w.pool, "ana-pg3@n.test", "Ana")
	w.handle(t, leo, "leo")
	do := w.pageClient(t)

	if rec := do(ana, http.MethodGet, "/app/u/leo"); strings.Contains(rec.Body.String(), "Unfollow") || !strings.Contains(rec.Body.String(), "Follow") {
		t.Fatal("expected a Follow button before following")
	}
	rec := do(ana, http.MethodPost, "/app/u/leo/follow")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/app/u/leo" {
		t.Fatalf("follow = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do(ana, http.MethodGet, "/app/u/leo"); !strings.Contains(rec.Body.String(), "Unfollow") {
		t.Fatal("no Unfollow after following")
	}
	if rec := do(ana, http.MethodPost, "/app/u/leo/unfollow"); rec.Code != http.StatusSeeOther {
		t.Fatalf("unfollow = %d", rec.Code)
	}
	if rec := do(ana, http.MethodGet, "/app/u/leo"); strings.Contains(rec.Body.String(), "Unfollow") {
		t.Fatal("still Unfollow after unfollowing")
	}
	// No button on your own page.
	if rec := do(leo, http.MethodGet, "/app/u/leo"); strings.Contains(rec.Body.String(), "/app/u/leo/follow") {
		t.Fatal("own page offers Follow")
	}
}

func TestBlockedProfilePageIs404(t *testing.T) {
	w := newWorld(t)
	leo, ana := person(t, w.pool, "leo-pg4@n.test", "Leo"), person(t, w.pool, "ana-pg4@n.test", "Ana")
	w.handle(t, leo, "leo")
	se := w.workout(t, leo, time.Hour)
	if err := w.soc.Block(context.Background(), leo.ID, ana.ID); err != nil {
		t.Fatal(err)
	}
	do := w.pageClient(t)
	for _, path := range []string{"/app/u/leo", "/app/u/leo/workouts", "/app/u/leo/workouts/" + se.ID.String(), "/app/u/nobody"} {
		if rec := do(ana, http.MethodGet, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d", path, rec.Code)
		}
	}
	if rec := do(ana, http.MethodPost, "/app/u/leo/follow"); rec.Code != http.StatusNotFound {
		t.Errorf("follow a blocker = %d", rec.Code)
	}
}

func TestRunCardHasNoZeroSets(t *testing.T) {
	w := newWorld(t)
	leo, ana := person(t, w.pool, "leo-pg5@n.test", "Leo"), person(t, w.pool, "ana-pg5@n.test", "Ana")
	w.handle(t, leo, "leo")
	se, err := w.acts.Log(context.Background(), leo.ID, activity.LogInput{
		ActivityCode: "running_8kmh", StartedAt: time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second), Duration: 30 * time.Minute, DistanceM: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	do := w.pageClient(t)
	for _, path := range []string{"/app/u/leo", "/app/u/leo/workouts", "/app/u/leo/workouts/" + se.ID.String()} {
		rec := do(ana, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "0 sets") {
			t.Errorf("%s shows 0 sets", path)
		}
		if !strings.Contains(rec.Body.String(), "5 km") {
			t.Errorf("%s lacks the distance", path)
		}
	}
}

func TestProfilePageAwardsStrip(t *testing.T) {
	w := newWorld(t)
	leo, ana := person(t, w.pool, "leo-st@n.test", "Leo"), person(t, w.pool, "ana-st@n.test", "Ana")
	w.handle(t, leo, "leost")
	aw := awards.NewService(awards.NewRepository(w.pool))
	at := time.Now().Add(-time.Hour)
	aw.Grant(context.Background(), leo.ID, at, "workouts_1")
	aw.Grant(context.Background(), leo.ID, at, "water_week") // private
	do := w.pageClient(t)

	other := do(ana, http.MethodGet, "/app/u/leost").Body.String()
	if !strings.Contains(other, "First Workout") || !strings.Contains(other, "/app/u/leost/awards") {
		t.Error("a follower's view lacks the shared badge or the link to all")
	}
	if strings.Contains(other, "A Week of Water") {
		t.Error("a private badge on another person's profile")
	}
	own := do(leo, http.MethodGet, "/app/u/leost").Body.String()
	if !strings.Contains(own, "A Week of Water") {
		t.Error("the owner does not see their private badge")
	}

	w.share(t, leo, achievements.Sharing{Training: true, XP: true, Awards: false})
	if strings.Contains(do(ana, http.MethodGet, "/app/u/leost").Body.String(), "First Workout") {
		t.Error("badges show with the awards switch off")
	}
}
