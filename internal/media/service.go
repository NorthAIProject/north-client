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
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/media/analysis"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/spend"
)

// MaxVideoBytes bounds an upload. Generous enough for a phone clip of a working
// set, small enough that one request cannot fill the disk.
const MaxVideoBytes int64 = 200 << 20 // 200 MB

// MaxImageBytes bounds a chat photo. Vision models accept a few megabytes;
// a 20 MB phone dump does not help the coach and blows the request.
const MaxImageBytes int64 = 8 << 20 // 8 MB

const (
	KindVideo = "video"
	KindImage = "image"
)

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

var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

type Service struct {
	repo     *Repository
	storage  Storage
	queue    *jobs.Queue
	registry *ai.Registry
	provider string
	model    string

	onReady func(ctx context.Context, userID, analysisID uuid.UUID)
}

type Options struct {
	Repository *Repository
	Storage    Storage
	Queue      *jobs.Queue
	Registry   *ai.Registry

	// Provider names the client used for analysis. Unlike the coach, this is a
	// single provider rather than a chain: the work needs a file upload API,
	// which only some of them have, so falling back to an arbitrary next
	// provider would fail rather than degrade.
	Provider string

	Model string

	// OnReady is called after a form analysis completes. Used to raise a
	// bell note. Nil is a no-op.
	OnReady func(ctx context.Context, userID, analysisID uuid.UUID)
}

func (s *Service) WithOnReady(fn func(ctx context.Context, userID, analysisID uuid.UUID)) *Service {
	s.onReady = fn
	return s
}

func NewService(opts Options) *Service {
	return &Service{
		repo:     opts.Repository,
		storage:  opts.Storage,
		queue:    opts.Queue,
		registry: opts.Registry,
		provider: opts.Provider,
		model:    opts.Model,
		onReady:  opts.OnReady,
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
	record, err := s.storeUpload(ctx, storeUpload{
		UserID:   userID,
		Filename: filename,
		Size:     size,
		Body:     body,
		Kind:     KindVideo,
		MaxBytes: MaxVideoBytes,
		Allowed:  allowedVideoTypes,
		Field:    "video",
		TooBig:   fmt.Sprintf("That file is over %d MB. Trim the clip to the working set.", MaxVideoBytes>>20),
		BadType:  "That does not look like a video Khepri can read. Upload an MP4, MOV, or WebM.",
	})
	if err != nil {
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

	if err := s.repo.CompleteAnalysis(ctx, p.AnalysisID, result, model, provider); err != nil {
		return err
	}
	if s.onReady != nil {
		if rec, recErr := s.repo.GetAnalysisByMedia(ctx, p.MediaID); recErr == nil {
			s.onReady(ctx, rec.UserID, rec.ID)
		}
	}
	return nil
}

func (s *Service) runAnalysis(ctx context.Context, mediaID uuid.UUID) (analysis.FormAnalysis, string, string, error) {
	record, err := s.repo.GetMediaByID(ctx, mediaID)
	if err != nil {
		return analysis.FormAnalysis{}, "", "", err
	}

	// Video is the most expensive thing per call that Khepri sends anywhere.
	// This is also the one AI path that bypasses ai.Runner, which is exactly
	// why metering lives on the client rather than in the runner.
	ctx = aiattr.WithUser(ctx, record.UserID, spend.SurfaceFormAnalysis)

	// Named rather than default: form analysis needs a provider with a real
	// upload API, and the OpenAI-dialect backends have none. Taking whatever
	// happens to head the chain would break video analysis every time the
	// coach was pointed at OpenRouter, NVIDIA, xAI, or Hermes.
	//
	// This is the one AI call in Khepri that does not go through ai.Runner, for
	// the same reason: the chain is a list of providers that can substitute for
	// one another, and here only one can do the job at all. There is nothing to
	// fall back to.
	client, err := s.registry.Get(s.provider)
	if err != nil {
		return analysis.FormAnalysis{}, "", "", apperr.Wrap(err,
			"media: analysis needs a provider that supports uploads")
	}

	object, err := s.storage.Get(ctx, record.StorageKey)
	if err != nil {
		return analysis.FormAnalysis{}, "", "", err
	}
	defer func() { _ = object.Close() }()

	// Uploaded to the provider from Khepri's own copy every time, rather than
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

// UploadImage stores a photo the coach should see this turn.
//
// Unlike UploadVideo it does not queue an analysis: the chat turn itself is
// the analysis. The file is just durable so the next photo can be compared
// and so the transcript can show what was sent.
func (s *Service) UploadImage(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (Media, error) {
	return s.storeUpload(ctx, storeUpload{
		UserID:   userID,
		Filename: filename,
		Size:     size,
		Body:     body,
		Kind:     KindImage,
		MaxBytes: MaxImageBytes,
		Allowed:  allowedImageTypes,
		Field:    "attachment",
		TooBig:   fmt.Sprintf("That photo is over %d MB. Send a smaller one.", MaxImageBytes>>20),
		BadType:  "That does not look like a photo I can read. Send a JPEG, PNG, GIF, or WebP.",
	})
}

type storeUpload struct {
	UserID   uuid.UUID
	Filename string
	Size     int64
	Body     io.Reader
	Kind     string
	MaxBytes int64
	Allowed  map[string]string
	Field    string
	TooBig   string
	BadType  string
}

func (s *Service) storeUpload(ctx context.Context, in storeUpload) (Media, error) {
	if in.Size > in.MaxBytes {
		return Media{}, apperr.FieldErrors{{Field: in.Field, Message: in.TooBig}}
	}

	// The declared content type is not trusted. http.DetectContentType reads the
	// leading bytes, so a .jpg extension on something else is caught here rather
	// than by the provider, or worse, not at all.
	header := make([]byte, 512)
	n, err := io.ReadFull(in.Body, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Media{}, apperr.Wrap(err, "read upload")
	}
	header = header[:n]

	mimeType := sniffMIME(header)
	extension, ok := in.Allowed[mimeType]
	if !ok {
		return Media{}, apperr.FieldErrors{{Field: in.Field, Message: in.BadType}}
	}

	mediaID := uuid.New()
	key := StorageKey(in.UserID, mediaID, extension)

	// The sniffed bytes are put back in front of the rest of the file.
	full := io.MultiReader(strings.NewReader(string(header)), in.Body)

	if putErr := s.storage.Put(ctx, key, mimeType, full); putErr != nil {
		return Media{}, putErr
	}

	record, err := s.repo.CreateMedia(ctx, NewMedia{
		UserID:       in.UserID,
		Kind:         in.Kind,
		MIMEType:     mimeType,
		SizeBytes:    in.Size,
		StorageKey:   key,
		OriginalName: filepath.Base(in.Filename),
	})
	if err != nil {
		// The object is orphaned rather than left half-registered. A stray
		// object costs storage; a media row pointing at nothing breaks playback.
		_ = s.storage.Delete(ctx, key)
		return Media{}, err
	}
	return record, nil
}

func (s *Service) GetMedia(ctx context.Context, id, userID uuid.UUID) (Media, error) {
	return s.repo.GetMedia(ctx, id, userID)
}

// LastImageAt is when they last sent a photo, if ever.
func (s *Service) LastImageAt(ctx context.Context, userID uuid.UUID) (time.Time, bool, error) {
	return s.repo.LatestCreatedAt(ctx, userID, KindImage)
}

// HasImage reports whether this person has uploaded a photo or a form clip.
func (s *Service) HasImage(ctx context.Context, userID uuid.UUID) (bool, error) {
	n, err := s.repo.CountByKind(ctx, userID, KindImage)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	n, err = s.repo.CountByKind(ctx, userID, KindVideo)
	return n > 0, err
}

// ReadBytes loads a stored file for the model. Images only: a video is too
// large to inline, and form analysis already has its own upload path.
func (s *Service) ReadBytes(ctx context.Context, userID, mediaID uuid.UUID) (string, []byte, error) {
	record, err := s.repo.GetMedia(ctx, mediaID, userID)
	if err != nil {
		return "", nil, err
	}
	if record.Kind != KindImage {
		return "", nil, apperr.Wrap(apperr.ErrValidation, "only photos can be sent inline")
	}

	object, err := s.storage.Get(ctx, record.StorageKey)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = object.Close() }()

	data, err := io.ReadAll(io.LimitReader(object, MaxImageBytes+1))
	if err != nil {
		return "", nil, apperr.Wrap(err, "read stored image")
	}
	if int64(len(data)) > MaxImageBytes {
		return "", nil, apperr.Wrap(apperr.ErrValidation, "image is too large to send to the coach")
	}
	return record.MIMEType, data, nil
}

// LoadInline is the name the coach calls. Same bytes as ReadBytes.
func (s *Service) LoadInline(ctx context.Context, userID, mediaID uuid.UUID) (string, []byte, error) {
	return s.ReadBytes(ctx, userID, mediaID)
}

// StoreChatImage / LoadChatImage are the names the chat handler calls.
//
// They exist so the coach package never imports this one: we already implement
// coach.ContextSource, and a reverse import would be a cycle.
func (s *Service) StoreChatImage(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (coach.ChatImage, error) {
	m, err := s.UploadImage(ctx, userID, filename, size, body)
	if err != nil {
		return coach.ChatImage{}, err
	}
	return toChatImage(m), nil
}

func (s *Service) LoadChatImage(ctx context.Context, id, userID uuid.UUID) (coach.ChatImage, error) {
	m, err := s.GetMedia(ctx, id, userID)
	if err != nil {
		return coach.ChatImage{}, err
	}
	return toChatImage(m), nil
}

func toChatImage(m Media) coach.ChatImage {
	return coach.ChatImage{
		ID:           m.ID,
		Kind:         m.Kind,
		MIMEType:     m.MIMEType,
		OriginalName: m.OriginalName,
	}
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

// sniffMIME prefers the leading bytes, then a WebP signature DetectContentType
// does not always name. A .webp that comes back as octet-stream would otherwise
// be refused even though every vision model we use can read it.
func sniffMIME(header []byte) string {
	mimeType := normaliseMIME(http.DetectContentType(header))
	if mimeType != "application/octet-stream" && mimeType != "text/plain" {
		return mimeType
	}
	if isWebP(header) {
		return "image/webp"
	}
	return mimeType
}

func isWebP(header []byte) bool {
	return len(header) >= 12 &&
		string(header[0:4]) == "RIFF" &&
		string(header[8:12]) == "WEBP"
}

// userFacing turns an internal failure into something worth showing.
func userFacing(err error) string {
	switch {
	case apperr.Is(err, apperr.ErrUnavailable):
		return "Khepri could not reach its AI provider. This will be retried automatically."
	case apperr.Is(err, apperr.ErrValidation):
		return "Khepri could not read that video. Try re-recording or converting it to MP4."
	default:
		return "Something went wrong analysing that video."
	}
}
