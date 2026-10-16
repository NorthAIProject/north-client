package progresschecks

import (
	"context"
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Compare describes what visibly changed between the person's first progress
// check with photos and their latest, and stores it, replacing the last
// comparison. The photos go to the same one provider an estimate would use,
// never the free chain.
//
// Front and side only: they are the poses a change shows in, and four photos
// keep the call small.
func (s *Service) Compare(ctx context.Context, user users.User) (Comparison, error) {
	dest, err := s.assist.destination(ctx, user.ID)
	if err != nil {
		return Comparison{}, err
	}
	first, latest, err := s.repo.FirstAndLatestWithPhotos(ctx, user.ID)
	if apperr.Is(err, apperr.ErrNotFound) || (err == nil && first.ID == latest.ID) {
		return Comparison{}, errNeedTwoChecks
	}
	if err != nil {
		return Comparison{}, err
	}

	var parts []ai.Part
	for _, side := range []struct {
		label string
		check Check
	}{{"First check", first}, {"Latest check", latest}} {
		images, photoErr := s.storedPhotos(ctx, user, side.check, side.label)
		if photoErr != nil {
			return Comparison{}, photoErr
		}
		if len(images) == 0 {
			// The files behind a check's photo rows are gone.
			return Comparison{}, errNeedTwoChecks
		}
		parts = append(parts, ai.TextPart(fmt.Sprintf("%s, %s.%s",
			side.label, side.check.MeasuredOn.Format("2 January 2006"), tapeLine(side.check))))
		parts = append(parts, images...)
	}
	parts = append(parts, ai.TextPart("Describe what changed between the first and the latest check. Write in "+language(user)+"."))

	var reply compareReply
	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfaceProgressCompare)
	if err = dest.readJSON(ctx, prompts.ProgressCompare, parts, compareSchema, 500, &reply); err != nil {
		return Comparison{}, err
	}
	cmp, ok := Comparison{
		FromCheckID: first.ID, ToCheckID: latest.ID,
		FromOn: first.MeasuredOn, ToOn: latest.MeasuredOn,
		Summary: reply.Summary, Confidence: reply.Confidence, Limits: reply.Limits,
		Provider: dest.Label,
	}.Clean()
	if !ok {
		return Comparison{}, fmt.Errorf("progress compare via %s: empty summary: %w", dest.client.Name(), apperr.ErrUnavailable)
	}
	if err = s.repo.SaveComparison(ctx, user.ID, cmp); err != nil {
		return Comparison{}, err
	}
	cmp.CreatedAt = s.now()
	return cmp, nil
}

// errNeedTwoChecks refuses a comparison with nothing to compare.
var errNeedTwoChecks = apperr.FieldErrors{}.Add("compare", "Comparing needs two checks with a front or side photo.")

// SavedComparison is the person's latest comparison, and whether a newer
// check with photos has come since. ok is false when there is none.
func (s *Service) SavedComparison(ctx context.Context, user users.User) (cmp Comparison, stale, ok bool, err error) {
	cmp, err = s.repo.Comparison(ctx, user.ID)
	if apperr.Is(err, apperr.ErrNotFound) {
		return Comparison{}, false, false, nil
	}
	if err != nil {
		return Comparison{}, false, false, err
	}
	_, latest, err := s.repo.FirstAndLatestWithPhotos(ctx, user.ID)
	if err != nil {
		return Comparison{}, false, false, err
	}
	return cmp, cmp.Stale(latest.ID), true, nil
}

// CanCompare reports whether the person has two checks with photos to compare.
func (s *Service) CanCompare(ctx context.Context, user users.User) (bool, error) {
	first, latest, err := s.repo.FirstAndLatestWithPhotos(ctx, user.ID)
	if apperr.Is(err, apperr.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return first.ID != latest.ID, nil
}

// tapeLine is a check's recorded measurements, for the model's context.
// A carried weight is not a reading and is left out.
func tapeLine(c Check) string {
	var parts []string
	for _, f := range []struct {
		name string
		v    *float64
		unit string
	}{
		{"waist", c.WaistCm, "cm"},
		{"hips", c.HipsCm, "cm"},
		{"chest", c.ChestCm, "cm"},
		{"thigh", c.ThighCm, "cm"},
		{"arm", c.ArmCm, "cm"},
	} {
		if f.v != nil {
			parts = append(parts, fmt.Sprintf("%s %.1f %s", f.name, *f.v, f.unit))
		}
	}
	if c.WeightKg != nil && !c.WeightCarried {
		parts = append(parts, fmt.Sprintf("weight %.1f kg", *c.WeightKg))
	}
	if len(parts) == 0 {
		return ""
	}
	return " Recorded that day: " + strings.Join(parts, ", ") + "."
}

// compareSchema has every field required, for OpenAI strict mode; limits is
// an empty string when nothing limited the comparison.
var compareSchema = ai.Object("What visibly changed between two progress checks.", map[string]*ai.Schema{
	"limits":     ai.String("One short line on what made the photos hard to compare, or an empty string."),
	"confidence": ai.Enum("How far the photos support the observation.", progresscheck.Confidences...),
	"summary":    ai.String("Two to four short neutral sentences on visible change, by area and pose."),
}, "limits", "confidence", "summary")

type compareReply struct {
	Limits     string `json:"limits"`
	Confidence string `json:"confidence"`
	Summary    string `json:"summary"`
}
