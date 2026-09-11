package capture

import (
	"io"
	"net/http"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	capturepages "github.com/NorthAIProject/north-client/web/capture"
)

// voiceMemoryLimit is how much of the upload is held in memory before the rest
// spills to a temporary file. Small on purpose: the bytes are going straight to
// a provider, not being served, so buffering the whole clip in RAM per request
// buys nothing.
const voiceMemoryLimit = 1 << 20

// voiceBodyLimit leaves room for the multipart envelope around the recording.
const voiceBodyLimit = MaxAudioBytes + 1<<20

// voice transcribes a recording and hands the words back to the composer.
//
// It renders the composer, never the preview. That is the whole safety argument
// of the feature: the person reads what was heard before anything is parsed
// from it, so a mis-heard number is caught by the one party who knows what they
// said. Rendering the preview here would save a tap and lose that.
func (h *Handler) voice(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	// Set before parsing so an oversized post is cut off rather than read.
	// The global MaxBody middleware is sized for form-check video, which is
	// twenty-five times what a minute of speech weighs.
	r.Body = http.MaxBytesReader(w, r.Body, voiceBodyLimit)

	if err := r.ParseMultipartForm(voiceMemoryLimit); err != nil {
		h.render(w, r, http.StatusRequestEntityTooLarge, capturepages.Data{
			Error: "That recording did not arrive properly. Try a shorter one.",
		})
		return
	}
	// ParseMultipartForm may have written a temporary file; nothing below reads
	// it twice, and leaving it behind would leak somebody's voice onto disk.
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, _, err := r.FormFile("audio")
	if err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, capturepages.Data{
			Error: "There was no recording in that.",
		})
		return
	}
	defer func() { _ = file.Close() }()

	// One byte past the ceiling, so the difference between "at the limit" and
	// "over it" is visible without reading the rest.
	audio, err := io.ReadAll(io.LimitReader(file, MaxAudioBytes+1))
	if err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, capturepages.Data{
			Error: "That recording did not arrive properly. Try again.",
		})
		return
	}

	text, err := h.svc.Transcribe(r.Context(), user, audio)
	if err != nil {
		// "Try again" is the wrong advice when nothing on this deployment can
		// listen — no ffmpeg for the container, or a chain whose providers are
		// all deaf or out of credit. Retrying will fail identically, and the
		// typed box on the same page works.
		if apperr.Is(err, apperr.ErrUnavailable) {
			h.render(w, r, statusFor(err), capturepages.Data{
				Error: "I cannot listen to recordings right now. Type it instead and everything else works as usual.",
			})
			return
		}
		h.render(w, r, statusFor(err), capturepages.Data{Error: message(err)})
		return
	}

	if text == "" {
		h.render(w, r, http.StatusOK, capturepages.Data{
			Error: "I could not hear anything in that.",
		})
		return
	}

	// The composer, with the words in the box. The person presses "Read it"
	// themselves, exactly as they would have after typing.
	h.render(w, r, http.StatusOK, capturepages.Data{Text: text})
}
