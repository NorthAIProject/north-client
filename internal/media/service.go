package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // stills arrive as JPEG; registered for DecodeConfig
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/media/analysis"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/spend"
)

// MaxVideoBytes bounds an upload. Enough for a phone clip of a working set,
// small enough that one request cannot fill the disk, and under Cloudflare's
// 100 MB request-body cap on kheprios.com's proxied zone with the multipart
// overhead included (platform/infra docs; north-web-app hosting-cloudflare.md).
const MaxVideoBytes int64 = 90 << 20 // 90 MB

// MaxImageBytes bounds a chat photo. Vision models accept a few megabytes;
// a 20 MB phone dump does not help the coach and blows the request.
const MaxImageBytes int64 = 8 << 20 // 8 MB

// MaxFileBytes bounds a document sent in chat: a diet PDF or a spreadsheet.
// Kept at a photo's size so one chat upload limit covers both.
const MaxFileBytes int64 = 8 << 20 // 8 MB

// Limits on the stills of one form check. They bound what one check can cost:
// a vision model bills by pixel, so the count and the edge together are the
// price, and the byte cap keeps the request body small enough to hold in
// memory while it is checked.
const (
	MaxFramesBytes int64 = 4 << 20 // 4 MB across every still
	MaxFrameEdge         = 1024    // pixels, the longer side
)

// stillsMaxTokens fits a summary and a handful of issues with room to spare.
const stillsMaxTokens = 1500

// stillsAttempts is one call and one retry on a reply that is not the JSON
// asked for.
const stillsAttempts = 2

const (
	KindVideo = "video"
	KindImage = "image"
	// KindFile is a document sent in chat, read as text rather than seen.
	KindFile = "file"
	// KindProgressPhoto is a body photo taken for a progress check. Its own
	// kind so it never counts as a chat photo, photo evidence or a document.
	KindProgressPhoto = "progress_photo"
	// KindFormFrame is a still cut from a form-check clip. Its own kind so it
	// never counts as a chat photo.
	KindFormFrame = "form_frame"
)

// ErrNoChatFile is LatestChatFile's answer when this person has sent no file,
// or none by the name asked for.
var ErrNoChatFile = apperr.Wrap(apperr.ErrNotFound, "no file has been sent in chat")

// signedURLLifetime is how long a playback link stays valid. Long enough to
// watch a clip several times, short enough that a leaked URL expires.
const signedURLLifetime = 2 * time.Hour

// analysisTemperature is low: this is observation, not composition. Used on
// the video path only; the stills path sends none, because Haiku 5.5 refuses
// any temperature but its default.
var analysisTemperature = float32(0.1)

var allowedFrameTypes = map[string]string{"image/jpeg": ".jpg"}

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

// fileType is a document extension the chat takes: what its leading bytes must
// sniff as, and the MIME type it is stored under.
type fileType struct {
	// sniff is the type http.DetectContentType must report, or "text" for any
	// UTF-8 text. Text formats have no signature, so a CSV can only be told
	// from a renamed photo by being text at all.
	sniff string
	mime  string
}

const sniffText = "text"

// allowedFileTypes are the documents planimport.ExtractText can read. The
// Office formats are zip containers and sniff as one; checking inside the zip
// is the reader's job.
var allowedFileTypes = map[string]fileType{
	".pdf":  {sniff: "application/pdf", mime: "application/pdf"},
	".docx": {sniff: "application/zip", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	".xlsx": {sniff: "application/zip", mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	".csv":  {sniff: sniffText, mime: "text/csv"},
	".tsv":  {sniff: sniffText, mime: "text/tab-separated-values"},
	".txt":  {sniff: sniffText, mime: "text/plain"},
	".md":   {sniff: sniffText, mime: "text/markdown"},
	".json": {sniff: sniffText, mime: "application/json"},
}

// AcceptsFileName reports whether UploadFile takes a document by this name,
// by its extension alone. For an adapter that must refuse a file before it
// downloads it; the bytes are still checked when it is stored.
func AcceptsFileName(filename string) bool {
	_, ok := allowedFileTypes[strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))]
	return ok
}

// imageExtensions route a chat upload to the photo path even when its bytes
// are not a photo we read, so the refusal talks about photos.
var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".heic": true, ".heif": true,
}

type Service struct {
	consent  ai.ConsentChecker
	repo     Repository
	storage  Storage
	queue    *jobs.Queue
	registry *ai.Registry
	provider string
	model    string
	vision   ai.Client

	onReady func(ctx context.Context, userID, analysisID uuid.UUID)
}

type Options struct {
	Repository Repository
	Storage    Storage
	Queue      *jobs.Queue
	Registry   *ai.Registry

	// Provider names the client used for analysis. Unlike the coach, this is a
	// single provider rather than a chain: the work needs a file upload API,
	// which only some of them have, so falling back to an arbitrary next
	// provider would fail rather than degrade.
	Provider string

	Model string

	// Vision reads the stills of a form check. One client, not a chain, for
	// the reason progresschecks.Assist gives: these are pictures of a person's
	// body, and they go to the one provider the consent screen names, never
	// to the free fallback chain. Nil means stills cannot be read here.
	Vision ai.Client

	// OnReady is called after a form analysis completes. Used to raise a
	// bell note. Nil is a no-op.
	OnReady func(ctx context.Context, userID, analysisID uuid.UUID)
}

// WithConsent checks AI data sharing before a form video is accepted. Nil allows.
func (s *Service) WithConsent(c ai.ConsentChecker) *Service {
	s.consent = c
	return s
}

func (s *Service) WithOnReady(fn func(ctx context.Context, userID, analysisID uuid.UUID)) *Service {
	s.onReady = fn
	return s
}

// Repository is the persistence Service needs. Service names it rather than
// the Postgres type so the database stays a detail of the composition root:
// NewRepository is what the process wires, and a test can hand Service
// anything that keeps uploaded media and their form analyses.
type Repository interface {
	CreateMedia(ctx context.Context, m NewMedia) (Media, error)
	CountByKind(ctx context.Context, userID uuid.UUID, kind string) (int, error)
	LatestCreatedAt(ctx context.Context, userID uuid.UUID, kind string) (time.Time, bool, error)
	GetMedia(ctx context.Context, id, userID uuid.UUID) (Media, error)
	DeleteMedia(ctx context.Context, id, userID uuid.UUID) (string, error)
	LatestOfKinds(ctx context.Context, userID uuid.UUID, kinds []string, name string) (Media, error)
	GetMediaByID(ctx context.Context, id uuid.UUID) (Media, error)
	CreateAnalysis(ctx context.Context, in NewAnalysis) (analysis.Analysis, error)
	ListFrames(ctx context.Context, analysisID uuid.UUID) ([]StoredFrame, error)
	GetAnalysis(ctx context.Context, id, userID uuid.UUID) (analysis.Analysis, error)
	GetAnalysisByMedia(ctx context.Context, mediaID uuid.UUID) (analysis.Analysis, error)
	ListAnalyses(ctx context.Context, userID uuid.UUID, limit int) ([]analysis.Analysis, error)
	StartAnalysis(ctx context.Context, id uuid.UUID) error
	CompleteAnalysis(ctx context.Context, id uuid.UUID, result analysis.FormAnalysis, model, provider string) error
	FailAnalysis(ctx context.Context, id uuid.UUID, reason string) error
}

func NewService(opts Options) *Service {
	return &Service{
		repo:     opts.Repository,
		storage:  opts.Storage,
		queue:    opts.Queue,
		registry: opts.Registry,
		provider: opts.Provider,
		model:    opts.Model,
		vision:   opts.Vision,
		onReady:  opts.OnReady,
	}
}

// AnalyzeFormPayload is the job payload.
type AnalyzeFormPayload struct {
	AnalysisID uuid.UUID `json:"analysis_id"`
	MediaID    uuid.UUID `json:"media_id"`
}

// FormUpload is one form check as a client sends it: the clip, kept for
// playback, and the stills cut from it, which are what gets read.
type FormUpload struct {
	Filename string
	Size     int64
	Body     io.Reader

	// DurationSeconds is the clip's length as the client measured it.
	DurationSeconds float64

	// ExerciseSlug is the catalogue exercise the person said they filmed.
	// Optional.
	ExerciseSlug string

	// Frames are the stills, in clip order. Empty from a client that predates
	// them.
	Frames []FrameUpload
}

// FrameUpload is one still and the second of the clip it was cut at.
type FrameUpload struct {
	AtSeconds float64
	Body      io.Reader
}

// UploadVideo stores a clip and its stills and queues the analysis.
//
// It returns as soon as the files are stored. Reading them takes longer than a
// request should, so the work happens in the worker and the page polls.
func (s *Service) UploadVideo(ctx context.Context, userID uuid.UUID, in FormUpload) (analysis.Analysis, error) {
	// Asked before the upload, so the analysis cannot be left failing later.
	if s.consent != nil {
		allowed, err := s.consent.AllowsAI(ctx, userID)
		if err != nil {
			return analysis.Analysis{}, err
		}
		if !allowed {
			return analysis.Analysis{}, apperr.Wrap(apperr.ErrAIConsentRequired, "upload form video")
		}
	}

	stills, err := readFrames(in)
	if err != nil {
		return analysis.Analysis{}, err
	}
	// Refused here rather than failed in the worker: nothing that can read
	// this upload exists, and storing 90 MB to say so later helps nobody.
	if !s.canRead(len(stills) > 0) {
		if len(stills) == 0 {
			return analysis.Analysis{}, apperr.FieldErrors{{Field: "video", Message: "This version of Khepri can no longer check form. Update the app and send the clip again."}}
		}
		return analysis.Analysis{}, apperr.Wrap(apperr.ErrUnavailable, "form check: no provider can read stills")
	}

	record, err := s.storeUpload(ctx, storeUpload{
		UserID:   userID,
		Filename: in.Filename,
		Size:     in.Size,
		Body:     in.Body,
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

	frames := make([]analysis.Frame, 0, len(stills))
	for i, still := range stills {
		stored, storeErr := s.storeUpload(ctx, storeUpload{
			UserID:   userID,
			Filename: fmt.Sprintf("frame-%02d.jpg", i+1),
			Size:     int64(len(still.data)),
			Body:     bytes.NewReader(still.data),
			Kind:     KindFormFrame,
			MaxBytes: MaxFramesBytes,
			Allowed:  allowedFrameTypes,
			Field:    "frames",
			TooBig:   "Those stills are too large.",
			BadType:  "The stills must be JPEG images.",
		})
		if storeErr != nil {
			return analysis.Analysis{}, storeErr
		}
		frames = append(frames, analysis.Frame{Index: i + 1, MediaID: stored.ID, AtSeconds: still.at})
	}

	pending, err := s.repo.CreateAnalysis(ctx, NewAnalysis{
		MediaID:         record.ID,
		UserID:          userID,
		DurationSeconds: in.DurationSeconds,
		ExerciseSlug:    strings.TrimSpace(in.ExerciseSlug),
		Frames:          frames,
	})
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

// canRead reports whether anything configured here can read this upload:
// the vision client for stills, or the upload provider for a bare video.
func (s *Service) canRead(hasStills bool) bool {
	if hasStills && s.vision != nil {
		return true
	}
	if s.provider == "" || s.registry == nil {
		return false
	}
	_, err := s.registry.Get(s.provider)
	return err == nil
}

// finite refuses the NaN and infinities strconv will happily parse; every
// comparison against NaN is false, so it would pass the range checks.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// VisionFrom picks the client that reads form-check stills: Khepri's own
// Anthropic client, the one provider the AI consent screen names.
//
// The scripted fake is always registered, so it is taken only where the
// deployment already runs on it (tests, local development). Anywhere else a
// missing Anthropic key must mean "cannot read stills", not a made-up answer.
// Nil when nothing may read them.
func VisionFrom(registry *ai.Registry) ai.Client {
	if registry == nil {
		return nil
	}
	if c, err := registry.Get("anthropic"); err == nil {
		return c
	}
	if registry.DefaultName() == "fake" {
		if c, err := registry.Get("fake"); err == nil {
			return c
		}
	}
	return nil
}

type still struct {
	at   float64
	data []byte
}

// readFrames reads and checks every still before anything is stored, so a bad
// one refuses the whole upload instead of leaving a check with gaps in it.
func readFrames(in FormUpload) ([]still, error) {
	if len(in.Frames) == 0 {
		return nil, nil
	}
	refuse := func(message string) ([]still, error) {
		return nil, apperr.FieldErrors{{Field: "frames", Message: message}}
	}
	if len(in.Frames) > analysis.MaxFrames {
		return refuse(fmt.Sprintf("Send at most %d stills.", analysis.MaxFrames))
	}
	if !finite(in.DurationSeconds) || in.DurationSeconds <= 0 {
		return refuse("Send the clip's length with its stills.")
	}

	out := make([]still, 0, len(in.Frames))
	var total int64
	last := -1.0
	for i, f := range in.Frames {
		// Strictly increasing and inside the clip: a marker is drawn at
		// at/duration, so anything else would land off the timeline.
		if !finite(f.AtSeconds) || f.AtSeconds < 0 || f.AtSeconds <= last || f.AtSeconds > in.DurationSeconds {
			return refuse("The stills must be in clip order, each inside the clip.")
		}
		last = f.AtSeconds

		data, err := io.ReadAll(io.LimitReader(f.Body, MaxFramesBytes-total+1))
		if err != nil {
			return nil, apperr.Wrap(err, "read still %d", i+1)
		}
		total += int64(len(data))
		if total > MaxFramesBytes {
			return refuse(fmt.Sprintf("Those stills are over %d MB together.", MaxFramesBytes>>20))
		}

		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "jpeg" {
			return refuse("The stills must be JPEG images.")
		}
		if max(cfg.Width, cfg.Height) > MaxFrameEdge {
			return refuse(fmt.Sprintf("Each still must be at most %d pixels on its longer side.", MaxFrameEdge))
		}
		out = append(out, still{at: f.AtSeconds, data: data})
	}
	return out, nil
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

	result, model, provider, err := s.runAnalysis(ctx, p)
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

// runAnalysis reads the check from its stills when it has any, and as video
// otherwise.
func (s *Service) runAnalysis(ctx context.Context, p AnalyzeFormPayload) (analysis.FormAnalysis, string, string, error) {
	record, err := s.repo.GetMediaByID(ctx, p.MediaID)
	if err != nil {
		return analysis.FormAnalysis{}, "", "", err
	}
	ctx = aiattr.WithUser(ctx, record.UserID, spend.SurfaceFormAnalysis)

	frames, err := s.repo.ListFrames(ctx, p.AnalysisID)
	if err != nil {
		return analysis.FormAnalysis{}, "", "", err
	}
	if len(frames) > 0 && s.vision != nil {
		return s.runStills(ctx, frames)
	}
	return s.runVideo(ctx, record)
}

// runStills shows the model the numbered stills and asks which of them show a
// fault. No temperature and no prefill: Haiku 5.5 refuses both.
func (s *Service) runStills(ctx context.Context, frames []StoredFrame) (analysis.FormAnalysis, string, string, error) {
	fail := func(err error) (analysis.FormAnalysis, string, string, error) {
		return analysis.FormAnalysis{}, "", "", err
	}

	parts := make([]ai.Part, 0, 2*len(frames)+1)
	plain := make([]analysis.Frame, 0, len(frames))
	for _, f := range frames {
		data, err := s.readFrame(ctx, f)
		if err != nil {
			return fail(err)
		}
		parts = append(parts,
			ai.TextPart(fmt.Sprintf("Still %d of %d, at %.1f s:", f.Index, len(frames), f.AtSeconds)),
			ai.Part{InlineData: data, MIMEType: f.MIMEType},
		)
		plain = append(plain, f.Frame)
	}
	parts = append(parts, ai.TextPart("Assess my form in these stills."))

	system, err := prompts.Raw(prompts.FormAnalysisStills)
	if err != nil {
		return fail(err)
	}

	messages := []ai.Message{{Role: ai.RoleUser, Parts: parts}}
	for attempt := 1; attempt <= stillsAttempts; attempt++ {
		resp, err := s.vision.Generate(ctx, ai.Request{
			System:         system,
			Messages:       messages,
			ResponseSchema: analysis.StillsSchema(),
			MaxTokens:      stillsMaxTokens,
		})
		if err != nil {
			return fail(fmt.Errorf("analyse stills via %s: %v: %w", s.vision.Name(), err, apperr.ErrUnavailable))
		}

		var result analysis.FormAnalysis
		if json.Unmarshal(ai.JSONReply(resp.Text), &result) == nil {
			// The model is whatever the provider says answered; the request
			// names none, so the client's own default applies.
			return analysis.FromStills(result, plain), resp.Model, s.vision.Name(), nil
		}
		messages = append(messages,
			ai.ModelText(resp.Text),
			ai.UserText("That was not valid JSON matching the schema. Answer again, correctly."),
		)
	}
	return fail(fmt.Errorf("analyse stills via %s: reply was not the JSON asked for: %w", s.vision.Name(), apperr.ErrUnavailable))
}

func (s *Service) readFrame(ctx context.Context, f StoredFrame) ([]byte, error) {
	object, err := s.storage.Get(ctx, f.StorageKey)
	if err != nil {
		return nil, apperr.Wrap(err, "open still %d", f.Index)
	}
	defer func() { _ = object.Close() }()

	data, err := io.ReadAll(io.LimitReader(object, MaxFramesBytes+1))
	if err != nil {
		return nil, apperr.Wrap(err, "read still %d", f.Index)
	}
	if int64(len(data)) > MaxFramesBytes {
		return nil, apperr.Wrap(apperr.ErrValidation, "still %d is larger than an upload allows", f.Index)
	}
	return data, nil
}

// runVideo is the path for a clip with no stills: the whole video goes to a
// provider with a file upload API. Kept for one release behind
// AI_UPLOAD_PROVIDER, for builds that send video only; production configures
// no such provider, and UploadVideo refuses those uploads before they get here.
func (s *Service) runVideo(ctx context.Context, record Media) (analysis.FormAnalysis, string, string, error) {
	// Named rather than default: this needs a provider with a real upload
	// API, and the OpenAI-dialect backends have none. It does not go through
	// ai.Runner for the same reason: the chain is a list of providers that
	// can substitute for one another, and here only one can do the job.
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
	if err := json.Unmarshal(ai.JSONReply(resp.Text), &result); err != nil {
		return analysis.FormAnalysis{}, "", "", apperr.Wrap(err, "decode analysis")
	}

	// Enforces what the prompt asks for. A model that lists faults while
	// admitting it cannot see the movement is guessing, and a guessed fault
	// sends someone away to fix a problem they do not have.
	result = analysis.Sanitise(result)
	result.Method = analysis.MethodVideo
	return result, s.model, client.Name(), nil
}

// FrameLink is one still of a check as a client shows it.
type FrameLink struct {
	Index     int
	AtSeconds float64
	URL       string
}

// FrameLinks are short-lived signed links to a check's stills, in clip order.
// Empty for a check that was read as video.
func (s *Service) FrameLinks(ctx context.Context, analysisID uuid.UUID) ([]FrameLink, error) {
	frames, err := s.repo.ListFrames(ctx, analysisID)
	if err != nil {
		return nil, err
	}
	out := make([]FrameLink, 0, len(frames))
	for _, f := range frames {
		url, err := s.storage.SignedURL(ctx, f.StorageKey, signedURLLifetime)
		if err != nil {
			return nil, apperr.Wrap(err, "sign still %d", f.Index)
		}
		out = append(out, FrameLink{Index: f.Index, AtSeconds: f.AtSeconds, URL: url})
	}
	return out, nil
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

// UploadProgressPhoto stores one pose of a progress check. Private like every
// upload: the key is under the user's prefix and only a signed URL reads it.
func (s *Service) UploadProgressPhoto(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (Media, error) {
	return s.storeUpload(ctx, storeUpload{
		UserID:   userID,
		Filename: filename,
		Size:     size,
		Body:     body,
		Kind:     KindProgressPhoto,
		MaxBytes: MaxImageBytes,
		Allowed:  allowedImageTypes,
		Field:    "photo",
		TooBig:   fmt.Sprintf("That photo is over %d MB. Choose a smaller one.", MaxImageBytes>>20),
		BadType:  "That does not look like a photo. Use a JPEG, PNG or WebP.",
	})
}

// DeleteOwned removes one of this person's files: the record, then the object.
// An object that will not delete is logged by the caller's error, not hidden;
// the record is gone either way, so nothing links to it again.
func (s *Service) DeleteOwned(ctx context.Context, userID, id uuid.UUID) error {
	key, err := s.repo.DeleteMedia(ctx, id, userID)
	if err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, key); err != nil {
		return apperr.Wrap(err, "delete stored object")
	}
	return nil
}

// UploadFile stores a document the coach should read this turn: a PDF, a Word
// or Excel file, or plain text.
//
// The extension decides what the file is meant to be, matched without regard
// to case because the iOS Files app keeps whatever name the file had. The
// bytes must then agree with it, so a photo renamed to .csv is refused here
// rather than read as garbage later.
func (s *Service) UploadFile(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (Media, error) {
	return s.storeUpload(ctx, storeUpload{
		UserID:    userID,
		Filename:  filename,
		Size:      size,
		Body:      body,
		Kind:      KindFile,
		MaxBytes:  MaxFileBytes,
		FileTypes: allowedFileTypes,
		Field:     "attachment",
		TooBig:    fmt.Sprintf("That file is over %d MB. Send a smaller one.", MaxFileBytes>>20),
		BadType:   "I can read PDF, Word (.docx), Excel (.xlsx), CSV, TSV, text, Markdown and JSON files, or a photo.",
	})
}

type storeUpload struct {
	UserID   uuid.UUID
	Filename string
	Size     int64
	Body     io.Reader
	Kind     string
	MaxBytes int64

	// Allowed maps a sniffed MIME type to the stored extension: the bytes
	// alone say what the file is. Used when FileTypes is nil.
	Allowed map[string]string

	// FileTypes maps a filename extension to what the bytes must sniff as.
	// Set for documents, whose text formats have no signature to go by.
	FileTypes map[string]fileType

	Field   string
	TooBig  string
	BadType string
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

	extension, mimeType, refusal := in.identify(header)
	if refusal != "" {
		return Media{}, apperr.FieldErrors{{Field: in.Field, Message: refusal}}
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

// identify names the extension and MIME type an upload is stored under, or
// says why it is refused.
func (in storeUpload) identify(header []byte) (extension, mimeType, refusal string) {
	if in.FileTypes != nil {
		return identifyFile(in.Filename, header, in.FileTypes, in.BadType)
	}
	mimeType = sniffMIME(header)
	extension, ok := in.Allowed[mimeType]
	if !ok {
		return "", "", in.BadType
	}
	return extension, mimeType, ""
}

func identifyFile(filename string, header []byte, types map[string]fileType, badType string) (extension, mimeType, refusal string) {
	extension = strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	want, ok := types[extension]
	if !ok {
		return "", "", badType
	}
	// Checked before the sniff, which calls nothing at all plain text.
	if len(header) == 0 {
		return "", "", "That file is empty."
	}

	sniffed := http.DetectContentType(header)
	if want.sniff == sniffText {
		// Any text type in UTF-8: a Markdown file that opens with an HTML
		// comment sniffs as text/html, and is still text. UTF-16 sniffs as
		// octet-stream or as another charset, and no reader here takes it.
		if !strings.HasPrefix(sniffed, "text/") || !strings.HasSuffix(sniffed, "charset=utf-8") {
			return "", "", fmt.Sprintf("That %s file is not plain UTF-8 text. Save it as UTF-8 and send it again.", extension)
		}
		return extension, want.mime, ""
	}
	if normaliseMIME(sniffed) != want.sniff {
		return "", "", fmt.Sprintf("That does not look like a real %s file. Export it again and send the new copy.", extension)
	}
	return extension, want.mime, ""
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
	if record.Kind != KindImage && record.Kind != KindProgressPhoto {
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

// ReadFile loads a photo or a document sent in chat, with its record, for a
// reader that needs the bytes: the coach reading a document's text, or an
// import of the plan in it. Another account's file, and a video, are not found.
func (s *Service) ReadFile(ctx context.Context, userID, id uuid.UUID) (Media, []byte, error) {
	record, err := s.chatMedia(ctx, id, userID)
	if err != nil {
		return Media{}, nil, err
	}
	data, err := s.readStored(ctx, record)
	if err != nil {
		return Media{}, nil, err
	}
	return record, data, nil
}

// LatestChatFile is the newest photo or document this person sent, with its
// bytes. A non-empty name picks the newest whose original filename matches it,
// ignoring case. Nothing sent, or nothing by that name, is ErrNoChatFile.
func (s *Service) LatestChatFile(ctx context.Context, userID uuid.UUID, name string) (Media, []byte, error) {
	record, err := s.repo.LatestOfKinds(ctx, userID, []string{KindFile, KindImage}, strings.TrimSpace(name))
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return Media{}, nil, ErrNoChatFile
		}
		return Media{}, nil, err
	}
	data, err := s.readStored(ctx, record)
	if err != nil {
		return Media{}, nil, err
	}
	return record, data, nil
}

// chatMedia is GetMedia limited to what a chat turn can carry.
func (s *Service) chatMedia(ctx context.Context, id, userID uuid.UUID) (Media, error) {
	record, err := s.repo.GetMedia(ctx, id, userID)
	if err != nil {
		return Media{}, err
	}
	if record.Kind != KindImage && record.Kind != KindFile {
		return Media{}, apperr.Wrap(apperr.ErrNotFound, "media %s is a %s, not a chat attachment", id, record.Kind)
	}
	return record, nil
}

// readStored reads a chat file's bytes, refusing more than an upload could
// have stored.
func (s *Service) readStored(ctx context.Context, record Media) ([]byte, error) {
	object, err := s.storage.Get(ctx, record.StorageKey)
	if err != nil {
		return nil, apperr.Wrap(err, "open stored file")
	}
	defer func() { _ = object.Close() }()

	limit := max(MaxFileBytes, MaxImageBytes)
	data, err := io.ReadAll(io.LimitReader(object, limit+1))
	if err != nil {
		return nil, apperr.Wrap(err, "read stored file")
	}
	if int64(len(data)) > limit {
		return nil, apperr.Wrap(apperr.ErrValidation, "stored file is larger than a chat upload")
	}
	return data, nil
}

// StoreChatAttachment / LoadChatAttachment are the names the chat handler
// calls.
//
// They exist so the coach package never imports this one: we already implement
// coach.ContextSource, and a reverse import would be a cycle.
//
// A photo is stored as one, for the model to see; anything else is a document,
// for the coach to read. A file named like a photo goes the photo's way even
// when its bytes are not one, so the refusal talks about photos.
func (s *Service) StoreChatAttachment(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (conversations.Attachment, error) {
	header := make([]byte, 512)
	n, err := io.ReadFull(body, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return conversations.Attachment{}, apperr.Wrap(err, "read upload")
	}
	header = header[:n]
	full := io.MultiReader(bytes.NewReader(header), body)

	upload := s.UploadFile
	_, isPhoto := allowedImageTypes[sniffMIME(header)]
	if isPhoto || imageExtensions[strings.ToLower(filepath.Ext(filename))] {
		upload = s.UploadImage
	}
	m, err := upload(ctx, userID, filename, size, full)
	if err != nil {
		return conversations.Attachment{}, err
	}
	return toChatAttachment(m), nil
}

// LoadChatAttachment is a photo or document this account sent, as the turn
// carries it. Another account's file, and a video, are not found.
func (s *Service) LoadChatAttachment(ctx context.Context, id, userID uuid.UUID) (conversations.Attachment, error) {
	m, err := s.chatMedia(ctx, id, userID)
	if err != nil {
		return conversations.Attachment{}, err
	}
	return toChatAttachment(m), nil
}

func toChatAttachment(m Media) conversations.Attachment {
	return conversations.Attachment{
		MediaID:  m.ID,
		Kind:     m.Kind,
		MIMEType: m.MIMEType,
		Name:     m.OriginalName,
	}
}

// SignedURL is a time-limited link to one of this person's files. Another
// account's file is not found.
func (s *Service) SignedURL(ctx context.Context, userID, id uuid.UUID) (string, error) {
	record, err := s.repo.GetMedia(ctx, id, userID)
	if err != nil {
		return "", err
	}
	return s.PlaybackURL(ctx, record)
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
		return "Khepri could not read that clip. Try re-recording or converting it to MP4."
	default:
		return "Something went wrong analysing that video."
	}
}
