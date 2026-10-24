package achievements

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/lifts"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the friends feed for native clients, what followers may see, and
// kudos. Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/feed", a.feed)
	r.Get("/social/sharing", a.getSharing)
	r.Put("/social/sharing", a.putSharing)
	r.Post("/achievements/{achievementID}/kudos", a.giveKudos)
	r.Delete("/achievements/{achievementID}/kudos", a.takeKudos)
}

type FeedItem struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"userId"`
	DisplayName string    `json:"displayName"`
	Handle      string    `json:"handle"`
	// Category is training, streaks or goals.
	Category   string    `json:"category"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail"`
	OccurredAt time.Time `json:"occurredAt"`
	Kudos      int       `json:"kudos"`
	Kudoed     bool      `json:"kudoed"`
	Mine       bool      `json:"mine"`
	// Workout is the finished workout, on a workout_completed item whose card
	// could be built.
	Workout *lifts.WorkoutCardView `json:"workout,omitempty"`
	// BadgeID, Area and Badges are on a badge_earned item (its category is
	// goals on the wire): the badge it is about, its area, and every badge
	// earned in the same moment, this one first, when there were several.
	BadgeID string       `json:"badgeId,omitempty"`
	Area    string       `json:"area,omitempty"`
	Badges  []award.View `json:"badges,omitempty"`
	// Monthly is on a monthly_challenge_met item (category goals on the wire):
	// the month and the challenge it met.
	Monthly *award.MonthlyMedalView `json:"monthly,omitempty"`
}

type Feed struct {
	Items []FeedItem `json:"items"`
	// Before is the cursor for the next page; absent on the last one.
	Before *time.Time `json:"before,omitempty"`
}

type SharingView struct {
	Training bool `json:"training"`
	Streaks  bool `json:"streaks"`
	Goals    bool `json:"goals"`
	// XP is XP and level, which puts the person on friends' leaderboards.
	XP bool `json:"xp"`
	// Awards is the badges the person has earned, except the private ones,
	// which are never shared.
	Awards bool `json:"awards"`
}

// SharingUpdate is SharingView with xp optional: a client from before the
// leaderboard does not send it, and its save must not switch it off.
type SharingUpdate struct {
	Training bool  `json:"training"`
	Streaks  bool  `json:"streaks"`
	Goals    bool  `json:"goals"`
	XP       *bool `json:"xp"`
	Awards   *bool `json:"awards"`
}

func (a *API) feed(w http.ResponseWriter, r *http.Request) {
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			httpx.Error(w, apperr.FieldErrors{}.Add("before", "Use an RFC 3339 time."), "That page could not be read.")
			return
		}
		before = t
	}
	items, next, err := a.svc.FeedPage(r.Context(), auth.MustUser(r.Context()).ID, before)
	if err != nil {
		httpx.Error(w, err, "The feed could not be loaded.")
		return
	}
	out := projectFeed(r.Context(), items)
	out.Before = next
	httpx.WriteJSON(w, http.StatusOK, out)
}

// feedExercises is how many exercises a feed card names.
const feedExercises = 2

// projectFeed is the wire feed. Before is the caller's to set, from FeedPage.
//
// The category enum on the wire is closed in the shipped app, so a badge item
// (stored under "awards") goes out as "goals"; the badge fields say what it is.
func projectFeed(ctx context.Context, items []Item) Feed {
	out := Feed{Items: make([]FeedItem, 0, len(items))}
	for _, it := range items {
		fi := FeedItem{
			ID: it.ID, UserID: it.UserID, DisplayName: it.DisplayName, Handle: it.Handle,
			Category: it.Category, Kind: it.Kind, Title: it.Title, Detail: it.Detail, OccurredAt: it.OccurredAt,
			Kudos: it.Kudos, Kudoed: it.Kudoed, Mine: it.Mine,
		}
		// The stored category is not on the closed wire enum, whatever the row.
		if it.Category == achievement.CategoryAwards {
			fi.Category = achievement.CategoryGoals
		}
		if it.BadgeID != "" {
			fi.Category = achievement.CategoryGoals
			fi.BadgeID = it.BadgeID
			ids := it.Badges
			if len(ids) == 0 {
				ids = []string{it.BadgeID}
			}
			for _, id := range ids {
				if b, ok := award.ByID(id); ok {
					if fi.Area == "" {
						fi.Area = string(b.Area)
					}
					at := it.OccurredAt
					fi.Badges = append(fi.Badges, b.Project(ctx, &at, nil, ""))
				}
			}
		}
		if it.Kind == achievement.KindMonthlyChallengeMet && it.MonthlyMetric != "" {
			fi.Category = achievement.CategoryGoals
			fi.Monthly = &award.MonthlyMedalView{Month: it.SourceKey, Metric: it.MonthlyMetric, Target: it.MonthlyTarget}
		}
		if it.Workout != nil {
			v := lifts.ProjectCard(*it.Workout, feedExercises)
			fi.Workout = &v
		}
		out.Items = append(out.Items, fi)
	}
	return out
}

func (a *API) getSharing(w http.ResponseWriter, r *http.Request) {
	s, err := a.svc.Sharing(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "What friends see could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SharingView(s))
}

func (a *API) putSharing(w http.ResponseWriter, r *http.Request) {
	var req SharingUpdate
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	userID := auth.MustUser(r.Context()).ID
	in := Sharing{Training: req.Training, Streaks: req.Streaks, Goals: req.Goals}
	// A client from before xp or awards does not send them; its save keeps
	// what is stored.
	current, err := a.svc.Sharing(r.Context(), userID)
	if err != nil {
		httpx.Error(w, err, "What friends see could not be saved.")
		return
	}
	in.XP, in.Awards = current.XP, current.Awards
	if req.XP != nil {
		in.XP = *req.XP
	}
	if req.Awards != nil {
		in.Awards = *req.Awards
	}
	s, err := a.svc.SetSharing(r.Context(), userID, in)
	if err != nil {
		httpx.Error(w, err, "What friends see could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SharingView(s))
}

func (a *API) giveKudos(w http.ResponseWriter, r *http.Request) {
	id, ok := achievementID(w, r)
	if !ok {
		return
	}
	user := auth.MustUser(r.Context())
	if err := a.svc.GiveKudos(r.Context(), user.ID, id, user.DisplayName); err != nil {
		httpx.Error(w, err, "Kudos could not be given.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) takeKudos(w http.ResponseWriter, r *http.Request) {
	id, ok := achievementID(w, r)
	if !ok {
		return
	}
	if err := a.svc.TakeKudos(r.Context(), auth.MustUser(r.Context()).ID, id); err != nil {
		httpx.Error(w, err, "Kudos could not be taken back.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func achievementID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "achievementID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}
