package voice

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/quota"
)

// API is dictation for clients of /api/v1: the same transcription the web's
// microphone uses, answered in the API's JSON error shape.
type API struct {
	handler *Handler
	quotas  *quota.Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service, quotas *quota.Service) *API {
	return &API{handler: NewHandler(svc, quotas), quotas: quotas}
}

// Register registers GET /voice, which says whether to offer a microphone.
func (a *API) Register(g *echo.Group) {
	g.GET("/voice", a.capabilities)
}

// RegisterUploads registers the route that takes a recording, on the upload
// group. It spends the same budget the web microphone does.
//
// The handler is the web microphone's own, wrapped rather than rewritten: it
// already answers in the API's JSON error shape, and one copy of the upload
// checks cannot drift from another.
func (a *API) RegisterUploads(g *echo.Group) {
	g.POST("/voice/transcribe", echo.WrapHandler(http.HandlerFunc(a.handler.Transcribe)), a.quotas.GuardAPI(quota.VoiceCapture))
}

// Capabilities is what a client needs to offer dictation, and its limits.
type Capabilities struct {
	// Available is false on a deployment with no transcriber: offer the
	// typed box only.
	Available  bool `json:"available"`
	MaxSeconds int  `json:"maxSeconds"`
	MaxBytes   int  `json:"maxBytes"`
}

func (a *API) capabilities(c *echo.Context) error {
	return c.JSON(http.StatusOK, Capabilities{
		Available:  a.handler.svc.Available(),
		MaxSeconds: MaxSeconds,
		MaxBytes:   MaxBytes,
	})
}
