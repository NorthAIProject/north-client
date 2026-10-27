// Package achievements records moments worth showing friends, decides who may
// see them, and keeps kudos. Recording happens where the moment happens
// (activity, check-ins, goals) through Recorder; nothing is entered by hand.
package achievements

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	achievementsdb "github.com/NorthAIProject/north-client/internal/achievements/db"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type (
	Item    = achievement.Item
	Sharing = achievement.Sharing
)

// Inbox tells somebody their achievement got kudos. nudges.Service satisfies
// it.
type Inbox interface {
	NoteWithPush(ctx context.Context, userID uuid.UUID, kind, dedupe, title, body, href string) error
}

// WorkoutCards builds the card of finished workouts, by session id. A session
// that is missing or not finished is absent from the result.
type WorkoutCards interface {
	CardsFor(ctx context.Context, sessionIDs []uuid.UUID) (map[uuid.UUID]lift.Card, error)
}

// KindKudos is the bell note for kudos received.
const KindKudos = "kudos"

// StreakMarks are the check-in streak lengths worth an achievement. Few on
// purpose: a moment every day is noise, and noise is what a feed must not be.
var StreakMarks = []int{7, 30, 100, 365}

// feedPage is how many items one request returns.
const feedPage = 30

type Service struct {
	q     *achievementsdb.Queries
	inbox Inbox
	cards WorkoutCards
	log   *slog.Logger
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{q: achievementsdb.New(pool), log: slog.Default()}
}

func (s *Service) WithInbox(in Inbox) *Service { s.inbox = in; return s }

// WithWorkoutCards lets the feed show a workout as a card, not a sentence.
func (s *Service) WithWorkoutCards(w WorkoutCards) *Service { s.cards = w; return s }

// record stores a moment once. Failures are logged, never returned to the
// caller: an achievement is a side effect of the thing that happened, and the
// thing that happened must not fail because of it.
func (s *Service) record(ctx context.Context, userID uuid.UUID, category, kind, sourceKey, title, detail string, at time.Time) {
	_, err := s.q.Record(ctx, achievementsdb.RecordParams{
		UserID: userID, Category: category, Kind: kind, Title: title, Detail: detail,
		OccurredAt: at, SourceKey: sourceKey,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.log.Warn("could not record achievement", "kind", kind, "user_id", userID, "error", err)
	}
}

// WorkoutCompleted records a finished session. sessionID keeps a session that
// is reported twice (stop, then a sync) to one achievement.
func (s *Service) WorkoutCompleted(ctx context.Context, userID, sessionID uuid.UUID, name string, minutes int, at time.Time) {
	detail := ""
	if minutes > 0 {
		detail = fmt.Sprintf("%d min", minutes)
	}
	s.record(ctx, userID, achievement.CategoryTraining, achievement.KindWorkoutCompleted, sessionID.String(),
		"Finished "+name, detail, at)
}

// StreakReached records a check-in streak, but only at StreakMarks.
func (s *Service) StreakReached(ctx context.Context, userID uuid.UUID, days int, localDate string, at time.Time) {
	for _, mark := range StreakMarks {
		if days == mark {
			s.record(ctx, userID, achievement.CategoryStreaks, achievement.KindStreakReached,
				fmt.Sprintf("checkin-%d-%s", days, localDate), fmt.Sprintf("%d-day check-in streak", days), "", at)
			return
		}
	}
}

// GoalCompleted records a goal marked achieved. A goal achieved, reopened and
// achieved again is still one achievement.
func (s *Service) GoalCompleted(ctx context.Context, userID, goalID uuid.UUID, title string, at time.Time) {
	s.record(ctx, userID, achievement.CategoryGoals, achievement.KindGoalCompleted, goalID.String(),
		"Achieved a goal: "+title, "", at)
}

// MilestoneReached records a milestone completed.
func (s *Service) MilestoneReached(ctx context.Context, userID, milestoneID uuid.UUID, title string, at time.Time) {
	s.record(ctx, userID, achievement.CategoryGoals, achievement.KindMilestoneReached, milestoneID.String(),
		"Reached a milestone: "+title, "", at)
}

// WeekReviewed records a weekly review finished, once per planned week. It
// sits with goals: a review is choosing what the goals get next.
func (s *Service) WeekReviewed(ctx context.Context, userID uuid.UUID, weekStart, at time.Time) {
	s.record(ctx, userID, achievement.CategoryGoals, achievement.KindWeekReviewed, weekStart.Format("2006-01-02"),
		"Planned the week of "+weekStart.Format("2 Jan"), "", at)
}

// AreaStreak records a life area on track for weeks consecutive weeks,
// once per run: the run's first Monday keys it, so a run that keeps going is
// not recorded again, and one that breaks and starts over is.
func (s *Service) AreaStreak(ctx context.Context, userID uuid.UUID, area, label string, weeks int, runStart, at time.Time) {
	s.record(ctx, userID, achievement.CategoryStreaks, achievement.KindAreaStreak,
		fmt.Sprintf("%s:%s", area, runStart.Format("2006-01-02")),
		fmt.Sprintf("%s on track %d weeks running", label, weeks), "", at)
}

// CrewChallengeMet records a crew's weekly challenge met by one member, once
// per member, crew and week. It sits with what the challenge counted: check-ins
// are a streak, workouts are training.
func (s *Service) CrewChallengeMet(ctx context.Context, userID, crewID uuid.UUID, crewName, kind string, target int, weekStart, at time.Time) {
	category, unit := achievement.CategoryStreaks, "check-ins"
	if kind == "workouts" {
		category, unit = achievement.CategoryTraining, "workouts"
	}
	s.record(ctx, userID, category, achievement.KindCrewChallengeMet,
		fmt.Sprintf("crew:%s:%s", crewID, weekStart.Format("2006-01-02")),
		"Met the "+crewName+" challenge",
		fmt.Sprintf("%d %s in the week of %s", target, unit, weekStart.Format("2 Jan")), at)
}

// Post shows a moment the person chose to share, unlike the side effects
// above: it reports a failure, and posting the same sourceKey again refreshes
// its title and detail in place.
func (s *Service) Post(ctx context.Context, userID uuid.UUID, category, kind, sourceKey, title, detail string, at time.Time) (Item, error) {
	row, err := s.q.Post(ctx, achievementsdb.PostParams{
		UserID: userID, Category: category, Kind: kind, Title: title, Detail: detail,
		OccurredAt: at, SourceKey: sourceKey,
	})
	if err != nil {
		return Item{}, apperr.Wrap(err, "post achievement")
	}
	return Item{
		ID: row.ID, UserID: userID, Category: category, Kind: kind, Title: title, Detail: detail,
		OccurredAt: row.OccurredAt, Mine: true,
	}, nil
}

// OwnBetween is the person's own achievements in [since, until), newest
// first, a badge in the request's locale and one card per moment.
func (s *Service) OwnBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Item, error) {
	rows, err := s.q.OwnAchievementsBetween(ctx, achievementsdb.OwnAchievementsBetweenParams{
		UserID: userID, Since: since, Until: until,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "own achievements")
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, Item{
			ID: r.ID, UserID: userID, Category: r.Category, Kind: r.Kind, Title: r.Title, Detail: r.Detail,
			OccurredAt: r.OccurredAt, Mine: true, SourceKey: r.SourceKey,
			MonthlyMetric: util.Val(r.MonthlyMetric), MonthlyTarget: int(util.Val(r.MonthlyTarget)),
		})
	}
	return foldMoments(ctx, out), nil
}

// Feed is the viewer's own achievements and those friends share with them,
// newest first, before the cursor (zero time means now).
func (s *Service) Feed(ctx context.Context, viewerID uuid.UUID, before time.Time) ([]Item, error) {
	items, _, err := s.FeedPage(ctx, viewerID, before)
	return items, err
}

// FeedPage is Feed plus the cursor for the next page, set only when the page
// was full. A moment (badges sharing one timestamp) is never split across
// pages: see the comment on the cursor below. Collapsing a moment's badges and
// dropping a streak mark make a full page come back with fewer items, which is
// why the cursor comes from the rows and not the items.
func (s *Service) FeedPage(ctx context.Context, viewerID uuid.UUID, before time.Time) ([]Item, *time.Time, error) {
	if before.IsZero() {
		before = time.Now().Add(time.Minute)
	}
	rows, err := s.q.Feed(ctx, achievementsdb.FeedParams{Viewer: viewerID, Before: before, Lim: feedPage})
	if err != nil {
		return nil, nil, apperr.Wrap(err, "feed")
	}
	// A moment's badges share one timestamp, and the next page starts strictly
	// before the cursor. So on a full page the trailing rows of the last
	// timestamp T are held back (unless the page is all T) and the cursor is
	// just after T: the next page begins with the whole moment.
	var next *time.Time
	if len(rows) == feedPage {
		last := rows[len(rows)-1].OccurredAt
		keep := len(rows)
		for keep > 0 && rows[keep-1].OccurredAt.Equal(last) {
			keep--
		}
		if keep > 0 {
			rows = rows[:keep]
			next = util.Ptr(last.Add(time.Microsecond))
		} else {
			next = util.Ptr(last)
		}
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, Item{
			ID: r.ID, UserID: r.UserID, DisplayName: r.DisplayName, Handle: util.Val(r.Handle),
			Category: r.Category, Kind: r.Kind, Title: r.Title, Detail: r.Detail, OccurredAt: r.OccurredAt,
			Kudos: int(r.Kudos), Kudoed: r.Kudoed, Mine: r.UserID == viewerID, SourceKey: r.SourceKey,
			Private: r.Private, MonthlyMetric: util.Val(r.MonthlyMetric), MonthlyTarget: int(util.Val(r.MonthlyTarget)),
		})
	}
	out = foldMoments(ctx, out)
	s.attachCards(ctx, out)
	return out, next, nil
}

// foldMoments makes a badge item read in the request's locale (never the
// English title stored with the row), puts the badges earned in one moment on
// one card, and drops a check-in streak mark whose streak badge is on the same
// day, which would say the same thing twice.
func foldMoments(ctx context.Context, items []Item) []Item {
	type day struct {
		user uuid.UUID
		date string
	}
	streakBadge := map[day]bool{}
	known := items[:0:0]
	for _, it := range items {
		// A badge the catalog no longer knows has no name to show and no area.
		if it.Kind == achievement.KindBadgeEarned {
			if _, ok := award.ByID(it.SourceKey); !ok {
				continue
			}
		}
		known = append(known, it)
	}
	items = known
	for i := range items {
		it := &items[i]
		if it.Kind == achievement.KindMonthlyChallengeMet && it.MonthlyMetric != "" {
			// Its own card: never merged with a badge, whatever the instant.
			it.Title = award.MonthlyMedalName(ctx, it.SourceKey)
			it.Detail = award.MonthlyMedalDescription(ctx, it.SourceKey, it.MonthlyMetric, it.MonthlyTarget)
			continue
		}
		if it.Kind != achievement.KindBadgeEarned {
			continue
		}
		b, _ := award.ByID(it.SourceKey)
		it.BadgeID = b.ID
		it.Title, it.Detail = b.Name(ctx), b.Description(ctx)
		if strings.HasPrefix(b.ID, "streak_") {
			streakBadge[day{it.UserID, it.OccurredAt.UTC().Format("2006-01-02")}] = true
		}
	}
	out := items[:0:0]
	for _, it := range items {
		if it.Kind == achievement.KindStreakReached && streakBadge[day{it.UserID, it.OccurredAt.UTC().Format("2006-01-02")}] {
			continue
		}
		if n := len(out); it.BadgeID != "" && n > 0 {
			if first := &out[n-1]; first.BadgeID != "" && first.UserID == it.UserID && first.OccurredAt.Equal(it.OccurredAt) {
				if first.Badges == nil {
					first.Badges = []string{first.BadgeID}
				}
				first.Badges = append(first.Badges, it.BadgeID)
				// Cheers on any badge of the moment count on its one card.
				first.Kudos += it.Kudos
				first.Kudoed = first.Kudoed || it.Kudoed
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// attachCards sets the card of each workout_completed item. It is best effort:
// an item whose source is not a session, a session that is gone, or a failed
// lookup leaves the plain item, and the feed is never failed over a card.
func (s *Service) attachCards(ctx context.Context, items []Item) {
	if s.cards == nil {
		return
	}
	var ids []uuid.UUID
	for _, it := range items {
		if it.Kind != achievement.KindWorkoutCompleted {
			continue
		}
		if id, err := uuid.Parse(it.SourceKey); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	cards, err := s.cards.CardsFor(ctx, ids)
	if err != nil {
		s.log.Warn("could not build workout cards for the feed", "error", err)
		return
	}
	for i := range items {
		id, err := uuid.Parse(items[i].SourceKey)
		if err != nil || items[i].Kind != achievement.KindWorkoutCompleted {
			continue
		}
		if c, ok := cards[id]; ok {
			items[i].Workout = &c
		}
	}
}

// Sharing is which categories the person's followers may see.
func (s *Service) Sharing(ctx context.Context, userID uuid.UUID) (Sharing, error) {
	rows, err := s.q.ListSharing(ctx, userID)
	if err != nil {
		return Sharing{}, apperr.Wrap(err, "list sharing")
	}
	out := Sharing{
		Training: achievement.DefaultShared(achievement.CategoryTraining),
		Streaks:  achievement.DefaultShared(achievement.CategoryStreaks),
		Goals:    achievement.DefaultShared(achievement.CategoryGoals),
		XP:       achievement.DefaultShared(achievement.CategoryXP),
		Awards:   achievement.DefaultShared(achievement.CategoryAwards),
	}
	for _, r := range rows {
		switch r.Category {
		case achievement.CategoryTraining:
			out.Training = r.Shared
		case achievement.CategoryStreaks:
			out.Streaks = r.Shared
		case achievement.CategoryGoals:
			out.Goals = r.Shared
		case achievement.CategoryXP:
			out.XP = r.Shared
		case achievement.CategoryAwards:
			out.Awards = r.Shared
		}
	}
	return out, nil
}

func (s *Service) SetSharing(ctx context.Context, userID uuid.UUID, in Sharing) (Sharing, error) {
	for category, shared := range map[string]bool{
		achievement.CategoryTraining: in.Training,
		achievement.CategoryStreaks:  in.Streaks,
		achievement.CategoryGoals:    in.Goals,
		achievement.CategoryXP:       in.XP,
		achievement.CategoryAwards:   in.Awards,
	} {
		if err := s.q.SetSharing(ctx, achievementsdb.SetSharingParams{UserID: userID, Category: category, Shared: shared}); err != nil {
			return Sharing{}, apperr.Wrap(err, "set sharing")
		}
	}
	return s.Sharing(ctx, userID)
}

// GiveKudos to an achievement the giver can see. One per person per
// achievement; the owner hears about each once. Not on your own.
func (s *Service) GiveKudos(ctx context.Context, giverID, achievementID uuid.UUID, giverName string) error {
	target, err := s.q.Visible(ctx, achievementsdb.VisibleParams{ID: achievementID, Viewer: giverID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.ErrNotFound
	}
	if err != nil {
		return apperr.Wrap(err, "check achievement")
	}
	if target.UserID == giverID {
		return apperr.FieldErrors{}.Add("achievement", "Kudos are for other people's moments.")
	}
	added, err := s.q.GiveKudos(ctx, achievementsdb.GiveKudosParams{AchievementID: achievementID, UserID: giverID})
	if err != nil {
		return apperr.Wrap(err, "give kudos")
	}
	if added > 0 && s.inbox != nil {
		_ = s.inbox.NoteWithPush(ctx, target.UserID, KindKudos, achievementID.String()+":"+giverID.String(),
			giverName+" gave you kudos", target.Title, "/app/friends")
	}
	return nil
}

func (s *Service) TakeKudos(ctx context.Context, giverID, achievementID uuid.UUID) error {
	if err := s.q.TakeKudos(ctx, achievementsdb.TakeKudosParams{AchievementID: achievementID, UserID: giverID}); err != nil {
		return apperr.Wrap(err, "take kudos")
	}
	return nil
}
