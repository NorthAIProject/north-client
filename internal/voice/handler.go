package voice

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/quota"
)

// Routes registers the endpoint. It spends the same budget the quick-capture
// recorder always did: it is the same act, on a different box.
func (h *Handler) Routes(r chi.Router) {
	r.With(h.quotas.Guard(quota.VoiceCapture)).Post("/voice/transcribe", h.Transcribe)
}

// Advertise tells the templates whether to offer a microphone at all.
//
// Carried on the context, the way the CSRF token is, so that the twenty-odd
// text boxes that offer dictation do not each need a field on their page data
// that every handler must remember to fill.
func (h *Handler) Advertise(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithDictation(r.Context(), h.svc.Available())))
	})
}

type dictationKey struct{}

// WithDictation records whether this deployment can transcribe. Exported so a
// template test can render the microphone without a handler in front of it.
func WithDictation(ctx context.Context, available bool) context.Context {
	return context.WithValue(ctx, dictationKey{}, available)
}

// DictationAvailable reports what WithDictation recorded. Absent means no: a
// page rendered outside the middleware offers the typed path only.
func DictationAvailable(ctx context.Context) bool {
	available, _ := ctx.Value(dictationKey{}).(bool)
	return available
}
