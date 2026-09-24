package voice

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/spend"
)

// memoryLimit is how much of the upload is held in memory before the rest
// spills to a temporary file. Small on purpose: the bytes are going straight to
// the recogniser, not being served, so buffering the whole clip in RAM per
// request buys nothing.
const memoryLimit = 1 << 20

// bodyLimit leaves room for the multipart envelope around the recording.
const bodyLimit = MaxBytes + 1<<20

// surfaces is every value a page may send as the surface field, and the spend
// label each one is recorded against.
//
// An allowlist rather than the value itself: the label lands in the spend
// ledger and in a metrics label, and letting a request body name either would
// let anybody invent a series. Anything not listed is dictation.
var surfaces = map[string]string{
	"capture": spend.SurfaceVoiceCapture,
}

// Handler turns a recording into text for whichever box the person was
// writing in.
//
// It answers JSON rather than HTML because it has no page of its own. The words
// go back into a textarea the browser already has, and the person sends them —
// nothing here submits anything on their behalf. That is the rule quick capture
// was built on: the person reads what was heard before anything acts on it.
type Handler struct {
	svc    *Service
	quotas *quota.Service
}

func NewHandler(svc *Service, quotas *quota.Service) *Handler {
	return &Handler{svc: svc, quotas: quotas}
}

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

// Transcript is the successful reply.
type Transcript struct {
	Text string `json:"text"`
}

// Transcribe reads the recording in the audio field and answers with its words.
func (h *Handler) Transcribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	// Before reading a byte. "Try again" is the wrong advice when nothing on
	// this deployment can listen: retrying fails identically, and the typed box
	// the button sits beside works.
	if !h.svc.Available() {
		httpx.Error(w, apperr.ErrUnavailable, i18n.T(ctx, "dictate.unavailable"))
		return
	}

	// Set before parsing so an oversized post is cut off rather than read. The
	// group's MaxBody is sized for form-check video, which is twenty-five times
	// what two minutes of speech weighs.
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)

	if err := r.ParseMultipartForm(memoryLimit); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.Error(w, httpx.ErrTooLarge, i18n.T(ctx, "dictate.toolong"))
			return
		}
		httpx.Error(w, apperr.ErrValidation, i18n.T(ctx, "dictate.unreadable"))
		return
	}
	// ParseMultipartForm may have written a temporary file; nothing below reads
	// it twice, and leaving it behind would leak somebody's voice onto disk.
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, _, err := r.FormFile("audio")
	if err != nil {
		httpx.Error(w, apperr.ErrValidation, i18n.T(ctx, "dictate.unreadable"))
		return
	}
	defer func() { _ = file.Close() }()

	// One byte past the ceiling, so the difference between "at the limit" and
	// "over it" is visible without reading the rest.
	recording, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		httpx.Error(w, apperr.ErrValidation, i18n.T(ctx, "dictate.unreadable"))
		return
	}
	if len(recording) > MaxBytes {
		httpx.Error(w, httpx.ErrTooLarge, i18n.T(ctx, "dictate.toolong"))
		return
	}

	text, err := h.svc.Transcribe(ctx, user, recording, surfaceFor(r.PostFormValue("surface")))
	if err != nil {
		switch {
		case apperr.Is(err, apperr.ErrUnavailable):
			httpx.Error(w, err, i18n.T(ctx, "dictate.unavailable"))
		case apperr.Is(err, apperr.ErrValidation):
			httpx.Error(w, err, i18n.T(ctx, "dictate.unreadable"))
		default:
			httpx.Error(w, err, i18n.T(ctx, "dictate.failed"))
		}
		return
	}

	// Silence is refused here rather than returned as an empty string, so the
	// browser has one path for "nothing to put in the box" and it comes with
	// words to show.
	if text == "" {
		httpx.Error(w, apperr.ErrValidation, i18n.T(ctx, "dictate.silent"))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, Transcript{Text: text})
}

// surfaceFor maps the page's surface field onto a spend label.
func surfaceFor(requested string) string {
	if label, ok := surfaces[requested]; ok {
		return label
	}
	return spend.SurfaceDictation
}
