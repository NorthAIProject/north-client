package progresschecks_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/progresschecks"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

var (
	checkID = uuid.MustParse("88888888-8888-8888-8888-888888888888")
	frontID = uuid.MustParse("99999999-9999-9999-9999-999999999991")
	sideID  = uuid.MustParse("99999999-9999-9999-9999-999999999992")
)

func goldenCheck() progresschecks.Check {
	medium := "medium"
	return progresschecks.Check{
		ID: checkID, MeasuredOn: date(2026, 10, 15),
		Measurements: progresscheck.Measurements{
			WeightKg: ptr(100), WaistCm: ptr(96), HipsCm: ptr(108), ChestCm: ptr(104),
		},
		Source: progresscheck.SourceAssisted, AssistConfidence: &medium,
		AssistNotes: "Waist estimate from the side photo; edited by you.",
		Photos: []progresschecks.Photo{
			{Pose: progresscheck.Front, MediaID: frontID},
			{Pose: progresscheck.Side, MediaID: sideID},
		},
		CreatedAt: time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC),
	}
}

func TestProgressChecksShape(t *testing.T) {
	t.Parallel()
	urls := map[uuid.UUID]string{
		frontID: "https://storage.test/front.jpg?sig=x",
		sideID:  "https://storage.test/side.jpg?sig=x",
	}
	view := progresschecks.ProgressChecksView{
		Checks: []progresschecks.ProgressCheckView{progresschecks.ProjectCheck(goldenCheck(), urls)},
		Schedule: progresschecks.ProjectSchedule(
			progresschecks.Schedule{Cadence: progresscheck.Weekly, NextDueOn: date(2026, 10, 22)}, false),
	}
	apitest.AssertGolden(t, "progress_checks.golden.json", view)
}

func TestBodyCurrentShape(t *testing.T) {
	t.Parallel()
	check := goldenCheck()
	bmi := 29.2
	body := progresschecks.Body{
		Check: &check, WeightKg: check.WeightKg, BMI: &bmi,
		Shape: progresscheck.Shape(check.Measurements, 185, bmi, progresscheckBaseline),
	}
	muscles := []lifts.BodyMapMuscle{
		{ID: "abs", Intensity: 0},
		{ID: "quads", Intensity: 0.62, LastTrainedOn: "2026-10-14"},
	}
	apitest.AssertGolden(t, "body_current.golden.json", progresschecks.ProjectBody(body, muscles))
}

// progresscheckBaseline is pinned here so the golden does not move when the
// mesh is rebuilt.
var progresscheckBaseline = bodymap.Baseline{
	Waist: bodymap.Girth{From: 0.43, To: 0.72},
	Hips:  bodymap.Girth{From: 0.52, To: 0.76},
	Chest: bodymap.Girth{From: 0.53, To: 0.74},
}

func TestProgressEstimateShape(t *testing.T) {
	t.Parallel()
	est := progresschecks.Estimate{WaistCm: ptr(95.2), HipsCm: ptr(104), Confidence: "medium", Notes: "Chest hidden by arms."}
	apitest.AssertGolden(t, "progress_estimate.golden.json", progresschecks.ProgressEstimateResult{
		Estimate: progresschecks.ProjectEstimate(est), ProviderLabel: "Anthropic (Claude)",
	})
}

func TestProgressAssistShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "progress_assist.golden.json", progresschecks.ProgressAssistView{
		Available: true, ProviderLabel: "OpenAI", OwnProvider: true,
	})
}

func TestProgressComparisonShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "progress_comparison.golden.json", progresschecks.ProjectComparison(progresschecks.Comparison{
		FromCheckID: uuid.MustParse("77777777-7777-7777-7777-777777777777"), ToCheckID: checkID,
		FromOn: date(2026, 9, 1), ToOn: date(2026, 10, 15),
		Summary:    "The waist looks narrower from the side; little visible change at the hips.",
		Confidence: "medium", Limits: "Different light in the first photos.",
		Provider: "Anthropic (Claude)", CreatedAt: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC),
	}, true))
}
