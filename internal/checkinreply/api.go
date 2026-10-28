package checkinreply

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the note under today's check-in for native clients. Mount behind
// auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

// Routes mounts the one endpoint. POST because asking may write the note:
// it is not safe to repeat for free the first time.
func (a *API) Routes(r chi.Router) {
	r.Post("/check-ins/today/reply", a.reply)
}

// ReplyView is one note.
type ReplyView struct {
	// Kind is daily or pattern. Open-ended; treat a value you do not know as
	// daily.
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func (a *API) reply(w http.ResponseWriter, r *http.Request) {
	reply, ok, err := a.svc.ForToday(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		httpx.Error(w, err, "The note could not be loaded.")
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ReplyView(reply))
}
