package dailyposts

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/dailyposts/post"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the daily post for native clients. Register behind auth.Bearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/daily-posts", a.feed)
	g.POST("/daily-posts/:postID/kudos", a.kudos(true))
	g.DELETE("/daily-posts/:postID/kudos", a.kudos(false))
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

func (a *API) feed(c *echo.Context) error {
	ctx := c.Request().Context()
	f, err := a.svc.Feed(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The daily post could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectFeed(f))
}

func (a *API) kudos(give bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := echox.UUIDParam(c, "postID")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		viewer := auth.MustUser(ctx).ID
		if give {
			err = a.svc.GiveKudos(ctx, id, viewer)
		} else {
			err = a.svc.TakeKudos(ctx, id, viewer)
		}
		if err != nil {
			return httpx.Fail(err, "Kudos could not be saved.")
		}
		return c.NoContent(http.StatusNoContent)
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
