package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/media/analysis"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// MaxVideoBytes bounds an upload. Generous enough for a phone clip of a working
// set, small enough that one request cannot fill the disk.
const MaxVideoBytes int64 = 200 << 20 // 200 MB

// signedURLLifetime is how long a playback link stays valid. Long enough to
// watch a clip several times, short enough that a leaked URL expires.
const signedURLLifetime = 2 * time.Hour

// analysisTemperature is low: this is observation, not composition.
var analysisTemperature = float32(0.1)

// allowedVideoTypes are the container formats a phone actually produces and
// Gemini actually reads.
var allowedVideoTypes = map[string]string{
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
	"video/webm":      ".webm",
}

type Service struct {
	repo     *Repository
	storage  Storage
	queue    *jobs.Queue
	registry *ai.Registry
	model    string
}

type Options struct {
	Repository *Repository
	Storage    Storage
	Queue      *jobs.Queue
	Registry   *ai.Registry
	Model      string
}

func NewService(opts Options) *Service {
	return &Service{
		repo:     opts.Repository,
		storage:  opts.Storage,
		queue:    opts.Queue,
		registry: opts.Registry,
		model:    opts.Model,
	}
}

// AnalyzeFormPayload is the job payload.
type AnalyzeFormPayload struct {
	AnalysisID uuid.UUID `json:"analysis_id"`
	MediaID    uuid.UUID `json:"media_id"`
}

// UploadVideo stores a clip and queues its analysis.
//
// It returns as soon as the file is stored. Analysing a video takes far longer
// than a request should, so the work happens in the worker and the page polls.
func (s *Service) UploadVideo(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (analysis.Analysis, error) {
	if size > MaxVideoBytes {
		return analysis.Analysis{}, apperr.FieldErrors{{
			Field:   "video",
			Message: fmt.Sprintf("That file is over %d MB. Trim the clip to the working set.", MaxVideoBytes>>20),
		}}
	}

	// The declared content type is not trusted. http.DetectContentType reads the
	// leading bytes, so a .mp4 extension on something else is caught here rather
	// than by the provider, or worse, not at all.
	header := make([]byte, 512)
	n, err := io.ReadFull(body, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return analysis.Analysis{}, apperr.Wrap(err, "read upload")
	}
	header = header[:n]

	mimeType := http.DetectContentType(header)
	extension, ok := allowedVideoTypes[normaliseMIME(mimeType)]
	if !ok {
		return analysis.Analysis{}, apperr.FieldErrors{{
			Field:   "video",
			Message: "That does not look like a video North can read. Upload an MP4, MOV, or WebM.",
		}}
	}

	mediaID := uuid.New()
	key := StorageKey(userID, mediaID, extension)

	// The sniffed bytes are put back in front of the rest of the file.
	full := io.MultiReader(strings.NewReader(string(header)), body)

	if err := s.storage.Put(ctx, key, normaliseMIME(mimeType), full); err != nil {
		return analysis.Analysis{}, err
	}

	record, err := s.repo.CreateMedia(ctx, NewMedia{
		UserID:       userID,
		Kind:         "video",
		MIMEType:     normaliseMIME(mimeType),
		SizeBytes:    size,
		StorageKey:   key,
		OriginalName: filepath.Base(filename),
	})
	if err != nil {
		// The object is orphaned rather than left half-registered. A stray
		// object costs storage; a media row pointing at nothing breaks playback.
		_ = s.storage.Delete(ctx, key)
		return analysis.Analysis{}, err
	}

	pending, err := s.repo.CreateAnalysis(ctx, record.ID, userID)
	if err != nil {
		return analysis.Analysis{}, err
	}

	if _, err := s.queue.Enqueue(ctx, jobs.KindAnalyzeFormVideo, AnalyzeFormPayload{
		AnalysisID: pending.ID,
		MediaID:    record.ID,
	}); err != nil {
		_ = s.repo.FailAnalysis(ctx, pending.ID, "could not queue the analysis")
		return analysis.Analysis{}, err
	}

	return pending, nil
}

// AnalyzeVideo is the job handler. It is registered with the worker.
func (s *Service) AnalyzeVideo(ctx context.Context, payload json.RawMessage) error {
	var p AnalyzeFormPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode analysis payload")
	}

	log := middleware.FromContext(ctx)

	if err := s.repo.StartAnalysis(ctx, p.AnalysisID); err != nil {
		return err
	}

	result, model, provider, err := s.runAnalysis(ctx, p.MediaID)
	if err != nil {
		// Recorded on the analysis as well as returned, so the page stops
		// spinning and says what happened, rather than polling forever.
		if failErr := s.repo.FailAnalysis(ctx, p.AnalysisID, userFacing(err)); failErr != nil {
			log.Error("could not record the analysis failure", "error", failErr)
		}
		return err
	}

	return s.repo.CompleteAnalysis(ctx, p.AnalysisID, result, model, provider)
}

func (s *Service) runAnalysis(ctx context.Context, mediaID uuid.UUID) (analysis.FormAnalysis, string, string, error) {
	record, err := s.repo.GetMediaByID(ctx, mediaID)
	if err != nil {
		return analysis.FormAnalysis{}, "", "", err
	}

	client, err := s.registry.Default()
	if err != nil {
		return analysis.FormAnalysis{}, "", "", err
	}

	object, err := s.storage.Get(ctx, record.StorageKey)
	if err != nil {
		return analysis.FormAnalysis{}, "", "", err
	}
	defer object.Close()

	// Uploaded to the provider from North's own copy every time, rather than
	// stored provider-side: Gemini deletes uploads within days, so the durable
	// copy has to be ours.
	file, err := client.UploadFile(ctx, ai.UploadRequest{
		Name:     record.OriginalName,
		MIMEType: record.MIMEType,
		Reader:   object,
	})
	if err != nil {
		return analysis.FormAnalysis{}, "", "", apperr.Wrap(err, "upload video to provider")
	}

	system, err := prompts.Raw(prompts.FormAnalysis)
	if err != nil {
		return analysis.FormAnalysis{}, "", "", err
	}

	resp, err := client.Generate(ctx, ai.Request{
		Model:          s.model,
		System:         system,
		ResponseSchema: analysis.Schema(),
		Temperature:    &analysisTemperature,
		Messages: []ai.Message{{
			Role: ai.RoleUser,
			Parts: []ai.Part{
				ai.FilePart(file.URI, file.MIMEType),
				ai.TextPart("Assess my form in this video."),
			},
		}},
	})
	if err != nil {
		return analysis.FormAnalysis{}, "", "", apperr.Wrap(err, "analyse video")
	}

	var result analysis.FormAnalysis
	if err := json.Unmarshal([]byte(resp.Text), &result); err != nil {
		return analysis.FormAnalysis{}, "", "", apperr.Wrap(err, "decode analysis")
	}

	// Enforces what the prompt asks for. A model that lists faults while
	// admitting it cannot see the movement is guessing, and a guessed fault
	// sends someone away to fix a problem they do not have.
	return analysis.Sanitise(result), s.model, client.Name(), nil
}

func (s *Service) GetAnalysis(ctx context.Context, id, userID uuid.UUID) (analysis.Analysis, error) {
	return s.repo.GetAnalysis(ctx, id, userID)
}

func (s *Service) ListAnalyses(ctx context.Context, userID uuid.UUID, limit int) ([]analysis.Analysis, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.repo.ListAnalyses(ctx, userID, limit)
}

func (s *Service) GetMedia(ctx context.Context, id, userID uuid.UUID) (Media, error) {
	return s.repo.GetMedia(ctx, id, userID)
}

// PlaybackURL is a time-limited link to the clip, so video bytes go straight
// from storage to the browser rather than through the application.
func (s *Service) PlaybackURL(ctx context.Context, m Media) (string, error) {
	return s.storage.SignedURL(ctx, m.StorageKey, signedURLLifetime)
}

// normaliseMIME strips the charset parameter DetectContentType can append.
func normaliseMIME(mimeType string) string {
	if base, _, found := strings.Cut(mimeType, ";"); found {
		return strings.TrimSpace(base)
	}
	return mimeType
}

// userFacing turns an internal failure into something worth showing.
func userFacing(err error) string {
	switch {
	case apperr.Is(err, apperr.ErrUnavailable):
		return "North could not reach its AI provider. This will be retried automatically."
	case apperr.Is(err, apperr.ErrValidation):
		return "North could not read that video. Try re-recording or converting it to MP4."
	default:
		return "Something went wrong analysing that video."
	}
}
