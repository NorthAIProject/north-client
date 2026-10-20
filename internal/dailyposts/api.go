package dailyposts

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/dailyposts/post"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the daily post for native clients. Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/daily-posts", a.feed)
	r.Post("/daily-posts/{postID}/kudos", a.kudos(true))
	r.Delete("/daily-posts/{postID}/kudos", a.kudos(false))
}

type PostView struct {
	ID uuid.UUID `json:"id"`
	// Date is the UTC day it is for, YYYY-MM-DD.
	Date string `json:"date"`
	// Theme is a short key such as sleep or hydration; an open set.
	Theme string `json:"theme"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// AIGenerated is true when the model wrote the post. Clients show it.
	AIGenerated bool `json:"aiGenerated"`
	Kudos       int  `json:"kudos"`
	Kudoed      bool `json:"kudoed"`
}

type FeedView struct {
	// Today is absent when today has no post yet, or none passed review.
	Today   *PostView  `json:"today,omitempty"`
	Earlier []PostView `json:"earlier"`
}

func (a *API) feed(w http.ResponseWriter, r *http.Request) {
	f, err := a.svc.Feed(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The daily post could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectFeed(f))
}

func (a *API) kudos(give bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "postID"))
		if err != nil {
			httpx.Error(w, apperr.ErrNotFound, "Not found.")
			return
		}
		viewer := auth.MustUser(r.Context()).ID
		if give {
			err = a.svc.GiveKudos(r.Context(), id, viewer)
		} else {
			err = a.svc.TakeKudos(r.Context(), id, viewer)
		}
		if err != nil {
			httpx.Error(w, err, "Kudos could not be saved.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func projectFeed(f Feed) FeedView {
	out := FeedView{Earlier: make([]PostView, 0, len(f.Earlier))}
	if f.Today != nil {
		v := projectPost(*f.Today)
		out.Today = &v
	}
	for _, p := range f.Earlier {
		out.Earlier = append(out.Earlier, projectPost(p))
	}
	return out
}

func projectPost(p post.Post) PostView {
	return PostView{
		ID: p.ID, Date: p.Date.Format(time.DateOnly), Theme: p.Theme, Title: p.Title, Body: p.Body,
		AIGenerated: p.AIGenerated, Kudos: p.Kudos, Kudoed: p.Kudoed,
	}
}
