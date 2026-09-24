package media

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/media/analysis"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is form checks for native clients: film a set, upload it, and read what
// the coach saw. The same service and quota as the web /form page.
type API struct {
	svc    *Service
	quotas *quota.Service
}

// NewAPI builds the routes. Routes go behind auth.RequireBearer; UploadRoutes
// go in the upload group, whose cap fits a phone clip.
func NewAPI(svc *Service, quotas *quota.Service) *API { return &API{svc: svc, quotas: quotas} }

func (a *API) Routes(r chi.Router) {
	r.Get("/form-checks", a.list)
	r.Get("/form-checks/{analysisID}", a.show)
}

func (a *API) UploadRoutes(r chi.Router) {
	r.With(a.quotas.GuardJSON(quota.MediaAnalysis)).Post("/form-checks", a.upload)
}

type FormIssueView struct {
	// At is where in the clip it is visible, in seconds.
	At          float64 `json:"at"`
	Severity    string  `json:"severity"`
	Observation string  `json:"observation"`
	Correction  string  `json:"correction"`
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
}

type FormCheckList struct {
	Checks []FormCheckView `json:"checks"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.svc.ListAnalyses(r.Context(), auth.MustUser(r.Context()).ID, 30)
	if err != nil {
		httpx.Error(w, err, "Form checks could not be loaded.")
		return
	}
	out := FormCheckList{Checks: make([]FormCheckView, 0, len(list))}
	for _, an := range list {
		out.Checks = append(out.Checks, project(an))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "analysisID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	an, err := a.svc.GetAnalysis(r.Context(), id, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The form check could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(an))
}

// upload takes one video as multipart/form-data, field "video", and answers
// 202 with the check that will analyse it; poll it until done or failed.
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxVideoBytes+(1<<20))
	file, header, err := r.FormFile("video")
	if err != nil {
		httpx.Error(w, apperr.FieldErrors{}.Add("video", "Choose a video under 200 MB."), "Choose a video under 200 MB.")
		return
	}
	defer func() { _ = file.Close() }()
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	an, err := a.svc.UploadVideo(r.Context(), auth.MustUser(r.Context()).ID, header.Filename, header.Size, file)
	if err != nil {
		httpx.Error(w, err, "The video could not be uploaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, project(an))
}

func project(an analysis.Analysis) FormCheckView {
	out := FormCheckView{ID: an.ID, Status: an.Status, Error: an.Error, CreatedAt: an.CreatedAt}
	if an.Result != nil {
		res := FormResultView{Exercise: an.Result.Exercise, Confidence: an.Result.Confidence, Summary: an.Result.Summary, Issues: []FormIssueView{}}
		for _, is := range an.Result.Issues {
			res.Issues = append(res.Issues, FormIssueView{At: is.TimestampSeconds, Severity: is.Severity, Observation: is.Observation, Correction: is.Correction})
		}
		out.Result = &res
	}
	return out
}
