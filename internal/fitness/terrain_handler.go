package fitness

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	fitnesspages "github.com/NorthAIProject/north-client/web/fitness"
)

// Paging the terrain.
//
// JSON rather than an HTMX fragment: the consumer is a WebGL surface with no
// DOM to swap into, and returning markup would mean parsing HTML to read data
// attributes back out — an encoding round trip with a worse failure mode. The
// list beside the scene pages over HTMX separately, against the same service
// call, so row markup never has to exist in both a .templ file and a client
// template.
//
// This never serves the first page. That one is rendered into the document,
// so the scene draws without a round trip and the page still works when this
// endpoint is down.

// terrainWeeksParam is how many weeks one request may ask for. The service
// clamps it too; this is the earlier, cheaper no.
const terrainWeeksParam = "weeks"

// terrainCursor is the query parameter naming where to page from.
const terrainCursor = "before"

// terrain serves one page of terrain as JSON.
func (h *Handler) terrain(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	before, weeks, err := terrainQuery(r, user.Location())
	if err != nil {
		httpx.Error(w, err, "That is not a date this page can start from.")
		return
	}

	page, err := h.strava.Terrain(ctx, user.ID, user.Location(), before, weeks)
	if err != nil {
		middleware.FromContext(ctx).Error("build activity terrain", slog.Any("error", err))
		httpx.Error(w, err, "Your activities could not be read just now.")
		return
	}

	// Somebody's training history. Never a shared cache, never a disk copy.
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.WriteJSON(w, http.StatusOK, fitnesspages.NewTerrainPayload(page))
}

// terrainList serves a page of weeks as the markup the strip swaps in.
//
// The same service call as the JSON endpoint, rendered instead of marshalled.
// Row markup then exists in one .templ file rather than in a template and a
// client-side renderer that have to agree.
func (h *Handler) terrainList(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	before, weeks, err := terrainQuery(r, user.Location())
	if err != nil {
		http.Error(w, "That is not a date this page can start from.", http.StatusUnprocessableEntity)
		return
	}

	page, err := h.strava.Terrain(ctx, user.ID, user.Location(), before, weeks)
	if err != nil {
		middleware.FromContext(ctx).Error("build activity strip", slog.Any("error", err))
		http.Error(w, "Your activities could not be read just now.", httpx.Status(err))
		return
	}

	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := fitnesspages.ActivityStrip(page, user.Location()).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render activity strip", slog.Any("error", err))
	}
}

// terrainQuery reads the cursor and the page size.
//
// The cursor is a local calendar date, which is stable, linkable and carries
// no instant that could be read differently in another zone. A date that is
// not a Monday is snapped back to the Monday on or before it rather than
// rejected: a hand-edited URL asking for a Wednesday means "the week holding
// that Wednesday", and answering it is friendlier than a 422 nobody can act
// on. A date that cannot be parsed, or one in the future, is a real error.
func terrainQuery(r *http.Request, loc *time.Location) (time.Time, int, error) {
	weeks := 0
	if raw := r.URL.Query().Get(terrainWeeksParam); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return time.Time{}, 0, apperr.Wrap(apperr.ErrValidation, "fitness: weeks must be a number")
		}
		weeks = n
	}

	raw := r.URL.Query().Get(terrainCursor)
	if raw == "" {
		return time.Time{}, weeks, nil
	}

	at, err := time.ParseInLocation(time.DateOnly, raw, loc)
	if err != nil {
		return time.Time{}, 0, apperr.Wrap(apperr.ErrValidation, "fitness: %s must be a date like 2026-09-07", terrainCursor)
	}
	if at.After(time.Now()) {
		return time.Time{}, 0, apperr.Wrap(apperr.ErrValidation, "fitness: %s cannot be in the future", terrainCursor)
	}

	// The cursor names the oldest week already drawn, so the page before it
	// ends where that week starts.
	return timerange.StartOfWeek(at), weeks, nil
}
