package strava

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The session list.
//
// A flat, numbered list of what somebody did, newest first — the training log
// read the way a training log is read. It is deliberately not the terrain's
// paging: the terrain walks weeks, because a week is the unit it lays out and
// a cursor is the only way to extend ground without moving what is already
// drawn. A reader clicking "page 3" wants the third ten sessions, and that
// question has no week-shaped answer.

const (
	// SessionsPerPage is how many sessions one page shows.
	SessionsPerPage = 10

	// maxSessionsPerPage bounds what a hand-edited URL may ask for, so the
	// page size cannot be turned into "give me everything".
	maxSessionsPerPage = 50
)

// SessionPage is one page of the list, and everything the pager needs to draw
// itself: which page this is, how many there are, and the rows.
type SessionPage struct {
	Sessions []Session

	// Page is 1-based, as it is written in the interface and in the URL.
	Page       int
	PerPage    int
	TotalPages int
	Total      int
}

// HasPrevious and HasNext keep the arithmetic out of the template.
func (p SessionPage) HasPrevious() bool { return p.Page > 1 }
func (p SessionPage) HasNext() bool     { return p.Page < p.TotalPages }

// First and Last are the 1-based positions this page covers, for "11–20 of
// 307". Both are zero when there is nothing.
func (p SessionPage) First() int {
	if len(p.Sessions) == 0 {
		return 0
	}
	return (p.Page-1)*p.PerPage + 1
}

func (p SessionPage) Last() int {
	if len(p.Sessions) == 0 {
		return 0
	}
	return p.First() + len(p.Sessions) - 1
}

// Sessions reads one page of somebody's activities.
//
// Reads North's own copy rather than calling Strava, like the rest of this
// page: opening it is fast, it works when Strava is down, and it costs nothing
// against the rate limit.
//
// A page number past the end is clamped to the last page rather than returning
// an empty list, so a stale link or a hand-edited URL lands somewhere real.
func (s *Service) Sessions(ctx context.Context, userID uuid.UUID, loc *time.Location, page, perPage int) (SessionPage, error) {
	if loc == nil {
		loc = time.UTC
	}
	if perPage <= 0 || perPage > maxSessionsPerPage {
		perPage = SessionsPerPage
	}

	total, err := s.repo.CountActivities(ctx, userID)
	if err != nil {
		return SessionPage{}, err
	}

	totalPages := (total + perPage - 1) / perPage
	if page < 1 {
		page = 1
	}
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}

	out := SessionPage{
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
		Total:      total,
	}
	if total == 0 {
		return out, nil
	}

	activities, err := s.repo.ActivitiesPage(ctx, userID, perPage, (page-1)*perPage)
	if err != nil {
		return SessionPage{}, err
	}

	out.Sessions = make([]Session, 0, len(activities))
	for _, a := range activities {
		out.Sessions = append(out.Sessions, sessionOf(a, loc))
	}
	return out, nil
}
