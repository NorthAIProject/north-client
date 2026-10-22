package lifts

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// CardsFor builds the workout card of each completed session in sessionIDs,
// keyed by session id. A session that is missing or not completed is absent.
//
// It is not owner-scoped: the caller must already have decided that the
// reader may see these workouts, which is why a feed or a profile asks for
// ids it chose rather than for anyone's.
func (s *Service) CardsFor(ctx context.Context, sessionIDs []uuid.UUID) (map[uuid.UUID]lift.Card, error) {
	out := map[uuid.UUID]lift.Card{}
	if len(sessionIDs) == 0 {
		return out, nil
	}
	if s.sessions == nil {
		return nil, errors.New("lifts: sessions not wired")
	}
	sessions, err := s.sessions.CompletedByIDs(ctx, sessionIDs)
	if err != nil {
		return nil, err
	}
	sessions = slices.DeleteFunc(sessions, func(se activity.Session) bool { return se.EndedAt == nil })
	if len(sessions) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(sessions))
	for i, se := range sessions {
		ids[i] = se.ID
	}
	all, err := s.repo.ListForSessions(ctx, ids)
	if err != nil {
		return nil, err
	}
	bySession := map[uuid.UUID][]Set{}
	for _, set := range all {
		bySession[*set.ActivitySessionID] = append(bySession[*set.ActivitySessionID], set)
	}

	histories, err := s.cardHistories(ctx, sessions, bySession)
	if err != nil {
		return nil, err
	}
	for _, se := range sessions {
		current := bySession[se.ID]
		out[se.ID] = lift.BuildCard(cardSession(se), current, earlierThan(histories[se.UserID], current, se.StartedAt))
	}
	return out, nil
}

// cardHistories is, per person, their sets of the exercises in these
// sessions from the year before the first of them, oldest first and uncapped.
// It costs one query per person however many sessions they have.
func (s *Service) cardHistories(ctx context.Context, sessions []activity.Session, bySession map[uuid.UUID][]Set) (map[uuid.UUID][]Set, error) {
	type window struct {
		first, last time.Time
		keys        map[string]bool
	}
	owners := map[uuid.UUID]*window{}
	for _, se := range sessions {
		w, ok := owners[se.UserID]
		if !ok {
			w = &window{first: se.StartedAt, last: se.StartedAt, keys: map[string]bool{}}
			owners[se.UserID] = w
		}
		if se.StartedAt.Before(w.first) {
			w.first = se.StartedAt
		}
		if se.StartedAt.After(w.last) {
			w.last = se.StartedAt
		}
		for _, set := range lift.Working(bySession[se.ID]) {
			w.keys[set.Key()] = true
		}
	}
	out := make(map[uuid.UUID][]Set, len(owners))
	for owner, w := range owners {
		if len(w.keys) == 0 {
			continue
		}
		keys := make([]string, 0, len(w.keys))
		for k := range w.keys {
			keys = append(keys, k)
		}
		hist, err := s.repo.ListForExercisesBetween(ctx, owner, keys, w.first.Add(-history), w.last)
		if err != nil {
			return nil, err
		}
		out[owner] = hist
	}
	return out, nil
}

// earlierThan is the part of a person's history that happened in the year
// before startedAt, leaving out the workout's own sets.
func earlierThan(hist, current []Set, startedAt time.Time) []Set {
	own := make(map[uuid.UUID]bool, len(current))
	for _, set := range current {
		own[set.ID] = true
	}
	since := startedAt.Add(-history)
	var out []Set
	for _, set := range hist {
		if !set.PerformedAt.Before(since) && set.PerformedAt.Before(startedAt) && !own[set.ID] {
			out = append(out, set)
		}
	}
	return out
}

func cardSession(se activity.Session) lift.CardSession {
	name := se.ActivityCode
	if met, ok := activity.LookupMET(se.ActivityCode); ok {
		name = met.Name
	}
	return lift.CardSession{
		ID:           se.ID,
		UserID:       se.UserID,
		Name:         name,
		ActivityCode: se.ActivityCode,
		StartedAt:    se.StartedAt,
		EndedAt:      *se.EndedAt,
		Duration:     se.Elapsed(*se.EndedAt),
		DistanceM:    se.DistanceM,
	}
}
