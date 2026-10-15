package progresschecks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

type Estimate = progresscheck.Estimate

// OwnProviders is the person's own connected provider. aicreds.Service
// satisfies it.
type OwnProviders interface {
	Get(ctx context.Context, userID uuid.UUID) (aicreds.Credential, error)
	For(ctx context.Context, userID uuid.UUID) (ai.Client, error)
}

// Assist decides where progress-check photos may go for an estimate.
//
// Two places only: the person's own connected provider, when its catalogue
// entry reads photos, or Khepri's own Anthropic client. Body photos never go
// to the free fallback chain, so this deliberately holds no ai.Runner. Nor
// does it fail over between the two: the page names one provider before
// anything is sent, and a silent switch would make that line untrue.
type Assist struct {
	own OwnProviders
	app ai.Client
}

// NewAssist takes either source as nil when it is not configured.
func NewAssist(own OwnProviders, app ai.Client) *Assist {
	return &Assist{own: own, app: app}
}

// Destination is the one provider an estimate would be sent to.
type Destination struct {
	// Label is the provider as a person knows it, for the consent line.
	Label string

	// Own is the person's own key, billed to their account.
	Own bool

	client ai.Client
}

// ErrAssistUnavailable means no provider may read these photos, and the page
// offers the manual form only.
var ErrAssistUnavailable = fmt.Errorf("progress estimate: no provider may read these photos: %w", apperr.ErrUnavailable)

// destination picks the provider. A connected provider that cannot see
// photos is passed over for Khepri's own client, not failed on.
func (a *Assist) destination(ctx context.Context, userID uuid.UUID) (Destination, error) {
	if a == nil {
		return Destination{}, ErrAssistUnavailable
	}
	if a.own != nil {
		cred, err := a.own.Get(ctx, userID)
		switch {
		case err == nil:
			if entry, ok := providers.ByName(cred.Provider); ok && entry.SeesImages {
				client, forErr := a.own.For(ctx, userID)
				if forErr != nil {
					return Destination{}, apperr.Wrap(forErr, "open connected provider")
				}
				if client != nil {
					return Destination{Label: entry.Label, Own: true, client: client}, nil
				}
			}
		case !apperr.Is(err, apperr.ErrNotFound):
			return Destination{}, apperr.Wrap(err, "read connected provider")
		}
	}
	if a.app != nil {
		label := "Anthropic (Claude)"
		if entry, ok := providers.ByName("anthropic"); ok {
			label = entry.Label
		}
		return Destination{Label: label, client: a.app}, nil
	}
	return Destination{}, ErrAssistUnavailable
}

// WithAssist lets the service estimate measurements from photos.
func (s *Service) WithAssist(a *Assist) *Service {
	s.assist = a
	return s
}

// AssistDestination is where this person's photos would go, so the page can
// name it before they press the button.
func (s *Service) AssistDestination(ctx context.Context, user users.User) (Destination, error) {
	return s.assist.destination(ctx, user.ID)
}

// maxEstimatePhoto is smaller than media's 8 MB: vision APIs refuse an image
// much over 5 MB once encoded, and a phone photo the page has downsized is a
// few hundred kilobytes.
const maxEstimatePhoto = 3_500_000

// estimateAttempts is one call and one retry on a reply that is not the JSON
// asked for.
const estimateAttempts = 2

// estimateMIME is what every provider in the catalogue reads.
var estimateMIME = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// Estimate reads waist, hips and chest off today's photos. Nothing is stored:
// the person sees the numbers in the form and saves what they accept.
//
// The newest earlier check with a tape-measured waist and photos goes along as
// a scale reference, and height when it is known.
func (s *Service) Estimate(ctx context.Context, user users.User, uploads []PhotoUpload) (Estimate, Destination, error) {
	dest, err := s.assist.destination(ctx, user.ID)
	if err != nil {
		return Estimate{}, Destination{}, err
	}
	today, err := readEstimatePhotos(uploads)
	if err != nil {
		return Estimate{}, dest, err
	}

	parts := []ai.Part{ai.TextPart("Today's photos.")}
	parts = append(parts, today...)
	reference, err := s.estimateReference(ctx, user)
	if err != nil {
		return Estimate{}, dest, err
	}
	parts = append(parts, reference...)
	locale := users.LocaleDefault
	if user.Locale.Valid() {
		locale = user.Locale
	}
	parts = append(parts, ai.TextPart("Estimate today's waist, hips and chest. Write the notes in "+locale.Language()+"."))

	system, err := prompts.Raw(prompts.ProgressEstimate)
	if err != nil {
		return Estimate{}, dest, apperr.Wrap(err, "load progress estimate prompt")
	}

	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfaceProgressEstimate)
	messages := []ai.Message{{Role: ai.RoleUser, Parts: parts}}
	for attempt := 1; attempt <= estimateAttempts; attempt++ {
		resp, genErr := dest.client.Generate(ctx, ai.Request{
			System:         system,
			Messages:       messages,
			ResponseSchema: estimateSchema,
			MaxTokens:      400,
		})
		if genErr != nil {
			return Estimate{}, dest, fmt.Errorf("progress estimate via %s: %v: %w", dest.client.Name(), genErr, apperr.ErrUnavailable)
		}

		var reply estimateReply
		if decErr := json.Unmarshal(ai.JSONReply(resp.Text), &reply); decErr == nil {
			return reply.estimate().Clean(), dest, nil
		}
		messages = append(messages,
			ai.ModelText(resp.Text),
			ai.UserText("That was not valid JSON matching the schema. Return the estimate again, correctly."),
		)
	}
	return Estimate{}, dest, fmt.Errorf("progress estimate via %s: reply was not the JSON asked for: %w", dest.client.Name(), apperr.ErrUnavailable)
}

// readEstimatePhotos checks each photo is one a provider can read. Front or
// side is needed: a back photo alone shows no waist from the front and no
// depth from the side.
func readEstimatePhotos(uploads []PhotoUpload) ([]ai.Part, error) {
	if err := distinctPoses(uploads); err != nil {
		return nil, err
	}
	var parts []ai.Part
	usable := false
	for _, u := range uploads {
		data, err := io.ReadAll(io.LimitReader(u.Body, maxEstimatePhoto+1))
		if err != nil {
			return nil, apperr.FieldErrors{}.Add(string(u.Pose), "That photo could not be read.")
		}
		if len(data) > maxEstimatePhoto {
			return nil, apperr.FieldErrors{}.Add(string(u.Pose), "That photo is too large to estimate from. Try a smaller one.")
		}
		mime := http.DetectContentType(data)
		if !estimateMIME[mime] {
			return nil, apperr.FieldErrors{}.Add(string(u.Pose), "Estimates need a JPEG, PNG or WebP photo.")
		}
		if u.Pose != progresscheck.Back {
			usable = true
		}
		parts = append(parts,
			ai.TextPart("Today, "+string(u.Pose)+":"),
			ai.Part{InlineData: data, MIMEType: mime},
		)
	}
	if !usable {
		return nil, apperr.FieldErrors{}.Add(string(progresscheck.Front), "Add a front or side photo to estimate from.")
	}
	return parts, nil
}

// estimateReference is the scale the model compares against: the newest
// earlier check whose waist was measured and that has a front or side photo,
// and the person's height. Either may be missing.
func (s *Service) estimateReference(ctx context.Context, user users.User) ([]ai.Part, error) {
	var parts []ai.Part

	checks, err := s.repo.List(ctx, user.ID, DefaultLimit)
	if err != nil {
		return nil, err
	}
	for _, c := range checks {
		if c.WaistCm == nil {
			continue
		}
		var images []ai.Part
		for _, pose := range []progresscheck.Pose{progresscheck.Front, progresscheck.Side} {
			photo, ok := c.Photo(pose)
			if !ok {
				continue
			}
			mime, data, readErr := s.photos.ReadBytes(ctx, user.ID, photo.MediaID)
			if apperr.Is(readErr, apperr.ErrNotFound) {
				continue
			}
			if readErr != nil {
				return nil, apperr.Wrap(readErr, "read earlier progress photo")
			}
			if !estimateMIME[mime] {
				continue
			}
			images = append(images, ai.TextPart("Earlier, "+string(pose)+":"), ai.Part{InlineData: data, MIMEType: mime})
		}
		if len(images) == 0 {
			continue
		}
		parts = append(parts, ai.TextPart(fmt.Sprintf(
			"Earlier photos from %s, when the tape measured the waist at %.1f cm.",
			c.MeasuredOn.Format("2 January 2006"), *c.WaistCm)))
		parts = append(parts, images...)
		break
	}

	bio, err := s.weights.Current(ctx, user.ID)
	switch {
	case err == nil && bio.HeightCm > 0:
		parts = append(parts, ai.TextPart(fmt.Sprintf("Height: %.0f cm.", bio.HeightCm)))
	case err != nil && !apperr.Is(err, apperr.ErrNotFound):
		return nil, err
	}
	return parts, nil
}

// estimateSchema has every field required: OpenAI's strict mode refuses a
// schema with optional ones. Zero stands for "not visible".
var estimateSchema = ai.Object("Tape measurements read off progress-check photos.", map[string]*ai.Schema{
	"notes":      ai.String("One short neutral line on what limited the estimate, or \"Clear photos.\""),
	"confidence": ai.Enum("How well the photos show the measurement lines.", progresscheck.Confidences...),
	"waist_cm":   ai.Number("Waist circumference at the navel in cm, or 0 when the photos do not show it."),
	"hips_cm":    ai.Number("Hip circumference at the widest point in cm, or 0 when the photos do not show it."),
	"chest_cm":   ai.Number("Chest circumference at nipple height in cm, or 0 when the photos do not show it."),
}, "notes", "confidence", "waist_cm", "hips_cm", "chest_cm")

type estimateReply struct {
	Notes      string  `json:"notes"`
	Confidence string  `json:"confidence"`
	WaistCm    float64 `json:"waist_cm"`
	HipsCm     float64 `json:"hips_cm"`
	ChestCm    float64 `json:"chest_cm"`
}

func (r estimateReply) estimate() Estimate {
	return Estimate{
		WaistCm: &r.WaistCm, HipsCm: &r.HipsCm, ChestCm: &r.ChestCm,
		Confidence: r.Confidence, Notes: strings.TrimSpace(r.Notes),
	}
}
