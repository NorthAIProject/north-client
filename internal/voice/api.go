package voice

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is dictation for clients of /api/v1: the same transcription the web's
// microphone uses, answered in the API's JSON error shape.
type API struct {
	handler *Handler
	quotas  *quota.Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service, quotas *quota.Service) *API {
	return &API{handler: NewHandler(svc, quotas), quotas: quotas}
}

// Routes mounts GET /voice, which says whether to offer a microphone.
func (a *API) Routes(r chi.Router) {
	r.Get("/voice", a.capabilities)
}

// UploadRoutes mounts the route that takes a recording; mount it in the upload
// group. It spends the same budget the web microphone does.
func (a *API) UploadRoutes(r chi.Router) {
	r.With(a.quotas.GuardJSON(quota.VoiceCapture)).Post("/voice/transcribe", a.handler.Transcribe)
}

// Capabilities is what a client needs to offer dictation, and its limits.
type Capabilities struct {
	// Available is false on a deployment with no transcriber: offer the
	// typed box only.
	Available  bool `json:"available"`
	MaxSeconds int  `json:"maxSeconds"`
	MaxBytes   int  `json:"maxBytes"`
}

func (a *API) capabilities(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, Capabilities{
		Available:  a.handler.svc.Available(),
		MaxSeconds: MaxSeconds,
		MaxBytes:   MaxBytes,
	})
}
