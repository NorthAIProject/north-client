package media

import (
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/media/analysis"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is form checks for native clients: film a set, upload it, and read what
// the coach saw. The same service and quota as the web /form page.
type API struct {
	svc    *Service
	quotas *quota.Service
}

// NewAPI builds the routes. Register goes behind auth.Bearer; RegisterUploads
// goes in the upload group, whose cap fits a phone clip.
func NewAPI(svc *Service, quotas *quota.Service) *API { return &API{svc: svc, quotas: quotas} }

func (a *API) Register(g *echo.Group) {
	g.GET("/form-checks", a.list)
	g.GET("/form-checks/:analysisID", a.show)
	g.GET("/media/:mediaID", a.link)
}

func (a *API) RegisterUploads(g *echo.Group) {
	g.POST("/form-checks", a.upload, a.quotas.GuardAPI(quota.MediaAnalysis))
}

type FormIssueView struct {
	// At is where in the clip it is visible, in seconds.
	At          float64 `json:"at"`
	Severity    string  `json:"severity"`
	Observation string  `json:"observation"`
	Correction  string  `json:"correction"`
	// FrameURL is a short-lived signed link to the still this was seen in.
	// Set only on the single-check route, for a check read from stills.
	FrameURL string `json:"frameUrl,omitempty"`
}

// FormFrameView is one still the check was read from.
type FormFrameView struct {
	// At is where in the clip it was cut, in seconds.
	At  float64 `json:"at"`
	URL string  `json:"url"`
}

type FormResultView struct {
	Exercise string `json:"exercise"`
	// Confidence is how clearly the footage shows the movement; low with no
	// issues means the angle cannot support a judgement.
	Confidence string          `json:"confidence"`
	Summary    string          `json:"summary"`
	Issues     []FormIssueView `json:"issues"`
}

type FormCheckView struct {
	ID uuid.UUID `json:"id"`
	// Status is pending, running, done or failed.
	Status    string          `json:"status"`
	Result    *FormResultView `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	// PlaybackURL is a short-lived signed link to the clip, set only on the
	// single-check route, so the player can seek to each issue's At.
	PlaybackURL string `json:"playbackUrl,omitempty"`
	// DurationSeconds is the clip's length, for placing each issue on a
	// timeline at at/durationSeconds. Absent on checks made before stills.
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	// Method is how the clip was read: "stills" (positions only, no tempo) or
	// "video". An open string; absent until the check is done.
	Method string `json:"method,omitempty"`
	// Frames are the stills the check was read from, in clip order. Set only
	// on the single-check route.
	Frames []FormFrameView `json:"frames,omitempty"`
}

type FormCheckList struct {
	Checks []FormCheckView `json:"checks"`
}

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	list, err := a.svc.ListAnalyses(ctx, auth.MustUser(ctx).ID, 30)
	if err != nil {
		return httpx.Fail(err, "Form checks could not be loaded.")
	}
	out := FormCheckList{Checks: make([]FormCheckView, 0, len(list))}
	for _, an := range list {
		out.Checks = append(out.Checks, project(an))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) show(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "analysisID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	an, err := a.svc.GetAnalysis(ctx, id, userID)
	if err != nil {
		return httpx.Fail(err, "The form check could not be loaded.")
	}
	clip, err := a.svc.GetMedia(ctx, an.MediaID, userID)
	if err != nil {
		return httpx.Fail(err, "The form check could not be loaded.")
	}
	playback, err := a.svc.PlaybackURL(ctx, clip)
	if err != nil {
		return httpx.Fail(err, "The form check could not be loaded.")
	}
	frames, err := a.svc.FrameLinks(ctx, an.ID)
	if err != nil {
		return httpx.Fail(err, "The form check could not be loaded.")
	}
	out := project(an)
	out.PlaybackURL = playback
	attachFrames(&out, an, frames)
	return c.JSON(http.StatusOK, out)
}

// attachFrames adds the stills to a check and, to each issue, the still it
// was seen in.
func attachFrames(out *FormCheckView, an analysis.Analysis, frames []FrameLink) {
	urls := make(map[int]string, len(frames))
	for _, f := range frames {
		out.Frames = append(out.Frames, FormFrameView{At: f.AtSeconds, URL: f.URL})
		urls[f.Index] = f.URL
	}
	if an.Result == nil || out.Result == nil {
		return
	}
	for i, issue := range an.Result.Issues {
		if i < len(out.Result.Issues) {
			out.Result.Issues[i].FrameURL = urls[issue.Frame]
		}
	}
}

// upload takes one check as multipart/form-data and answers 202 with the
// check that will read it; poll it until done or failed.
//
// Fields: "video" (the clip, kept for playback), "frames" (JPEG stills cut
// from it, repeated, in clip order), "frameTimes" (their seconds, comma
// separated, one per still), "durationSeconds" and optionally "exerciseSlug".
func (a *API) upload(c *echo.Context) error {
	r := c.Request()
	r.Body = http.MaxBytesReader(c.Response(), r.Body, MaxVideoBytes+MaxFramesBytes+(1<<20))
	// Explicit, because FormFile on its own parses with net/http's default of
	// 32 MiB in memory per upload. Past this the part spills to disk, as in every
	// other multipart handler.
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return httpx.Fail(apperr.FieldErrors{}.Add("video", "Choose a video under 90 MB."), "Choose a video under 90 MB.")
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("video")
	if err != nil {
		return httpx.Fail(apperr.FieldErrors{}.Add("video", "Choose a video under 90 MB."), "Choose a video under 90 MB.")
	}
	defer func() { _ = file.Close() }()

	in := FormUpload{
		Filename:     header.Filename,
		Size:         header.Size,
		Body:         file,
		ExerciseSlug: r.FormValue("exerciseSlug"),
	}
	closeFrames, err := readFrameParts(r.MultipartForm, &in)
	defer closeFrames()
	if err != nil {
		return httpx.Fail(err, "The stills could not be read.")
	}

	ctx := r.Context()
	an, err := a.svc.UploadVideo(ctx, auth.MustUser(ctx).ID, in)
	if err != nil {
		return httpx.Fail(err, "The video could not be uploaded.")
	}
	return c.JSON(http.StatusAccepted, project(an))
}

// readFrameParts fills the stills and the clip length from the form. It only
// pairs each still with its time; the service checks what they contain. The
// returned func closes the opened parts and is never nil.
func readFrameParts(form *multipart.Form, in *FormUpload) (func(), error) {
	var opened []multipart.File
	closeAll := func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}
	refuse := func(message string) (func(), error) {
		return closeAll, apperr.FieldErrors{}.Add("frames", message)
	}

	parts := form.File["frames"]
	if len(parts) == 0 {
		return closeAll, nil
	}
	if len(parts) > analysis.MaxFrames {
		return refuse("Too many stills.")
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(first(form.Value["durationSeconds"])), 64)
	if err != nil {
		return refuse("Send the clip's length with its stills.")
	}
	in.DurationSeconds = duration

	times := strings.Split(strings.Join(form.Value["frameTimes"], ","), ",")
	if len(times) != len(parts) {
		return refuse("Send one time for each still.")
	}
	for i, part := range parts {
		at, err := strconv.ParseFloat(strings.TrimSpace(times[i]), 64)
		if err != nil {
			return refuse("Send one time for each still.")
		}
		f, err := part.Open()
		if err != nil {
			return closeAll, apperr.Wrap(err, "open still %d", i+1)
		}
		opened = append(opened, f)
		in.Frames = append(in.Frames, FrameUpload{AtSeconds: at, Body: f})
	}
	return closeAll, nil
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// MediaLinkView is a short-lived signed link to one stored file: a chat photo,
// a progress photo or a form clip. The browser fetches the bytes from object
// storage, so they never stream through the app.
type MediaLinkView struct {
	ID  uuid.UUID `json:"id"`
	URL string    `json:"url"`
	// Kind is image, video or file; an open string.
	Kind     string `json:"kind"`
	MIMEType string `json:"mimeType"`
}

// link is the web's GET /app/media/{id} for API clients: the same owner check
// and the same signed URL, as JSON rather than a redirect.
func (a *API) link(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "mediaID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	record, err := a.svc.GetMedia(ctx, id, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The file could not be loaded.")
	}
	url, err := a.svc.PlaybackURL(ctx, record)
	if err != nil {
		return httpx.Fail(err, "The file could not be loaded.")
	}
	return c.JSON(http.StatusOK, MediaLinkView{ID: record.ID, URL: url, Kind: record.Kind, MIMEType: record.MIMEType})
}

func project(an analysis.Analysis) FormCheckView {
	out := FormCheckView{ID: an.ID, Status: an.Status, Error: an.Error, CreatedAt: an.CreatedAt, DurationSeconds: an.DurationSeconds}
	if an.Result != nil {
		out.Method = an.Result.Method
		res := FormResultView{Exercise: an.Result.Exercise, Confidence: an.Result.Confidence, Summary: an.Result.Summary, Issues: []FormIssueView{}}
		for _, is := range an.Result.Issues {
			res.Issues = append(res.Issues, FormIssueView{At: is.TimestampSeconds, Severity: is.Severity, Observation: is.Observation, Correction: is.Correction})
		}
		out.Result = &res
	}
	return out
}
