package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/capture"
)

// mountAPI owns the public JSON API boundary. Feature APIs register paths
// relative to /api/v1 so versioning stays centralized when more endpoints are
// added.
func mountAPI(r chi.Router, captureAPI *capture.API) {
	r.Route("/api/v1", func(r chi.Router) {
		captureAPI.Routes(r)
	})
}
