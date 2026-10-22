package profiles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/xp"
)

func card(id string, ended time.Time) lift.Card {
	return lift.Card{
		CardSession: lift.CardSession{
			ID: uuid.MustParse(id), UserID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Name: "Push day",
			ActivityCode: "strength_training_light", StartedAt: ended.Add(-time.Hour), EndedAt: ended, Duration: time.Hour,
		},
		Sets: 9, VolumeKg: 3200.5, PRs: 1,
		Exercises: []lift.CardExercise{
			{Name: "Bench press", Sets: 4, VolumeKg: 1600, Best: "100 kg × 5", PR: true},
			{Name: "Overhead press", Sets: 3, VolumeKg: 900, Best: "60 kg × 6"},
			{Name: "Dips", Sets: 2, VolumeKg: 700.5},
		},
	}
}

func TestProfileShapes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ended := time.Date(2026, 10, 20, 18, 30, 0, 0, time.UTC)
	cards := []lift.Card{card("11111111-1111-1111-1111-111111111111", ended), card("22222222-2222-2222-2222-222222222222", ended.Add(-48*time.Hour))}
	profile := social.Profile{
		Person:    social.Person{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), DisplayName: "Ana", Handle: "ana_runs"},
		Followers: 12, Following: 9,
		Relationship: social.Relationship{Following: social.StatusAccepted, FollowsYou: true},
	}
	level := xp.LevelFor(340)
	n := 14

	apitest.AssertGolden(t, "profile.golden.json", projectView(ctx, View{
		Profile: profile, Shares: Shares{Training: true, XP: true}, Level: &level, Workouts: &n, Recent: cards,
	}))
	badge, _ := award.ByID("workouts_1")
	apitest.AssertGolden(t, "profile-awards.golden.json", projectView(ctx, View{
		Profile: profile, Shares: Shares{Awards: true}, Awards: []awards.Earned{{Badge: badge, At: ended}},
	}))
	// A person who shares nothing: the old fields plus the switches, no more.
	apitest.AssertGolden(t, "profile-private.golden.json", projectView(ctx, View{Profile: profile}))
	before := ended.Add(-48 * time.Hour)
	apitest.AssertGolden(t, "profile-workouts.golden.json", projectPage(Page{Items: cards, Before: &before, Shared: true}))
	apitest.AssertGolden(t, "profile-workouts-private.golden.json", projectPage(Page{Items: []lift.Card{}}))
}

func TestWorkoutIDThatIsNotAUUIDIsNotFound(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	NewAPI(NewService(nil, nil, nil, nil, nil)).Routes(r)
	req := httptest.NewRequest(http.MethodGet, "/profiles/leo/workouts/not-a-uuid", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
