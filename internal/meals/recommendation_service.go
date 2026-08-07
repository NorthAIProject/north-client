package meals

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/calculator"
)

const (
	// recommendationWindowDays is how far back adherence and average delta
	// look. Two weeks is enough to smooth over a single bad day without
	// reacting to ancient history.
	recommendationWindowDays = 14

	// adherenceFloor below which the recommendation is "log more
	// consistently" rather than any adjustment to the target itself — an
	// inconsistent log cannot tell anyone whether the target is wrong.
	adherenceFloor = 0.5

	// driftThresholdKcal is how far the trailing average has to miss the
	// goal, in either direction, before it is worth a suggestion rather than
	// noise.
	driftThresholdKcal = 200
)

type GoalRecommendationService struct {
	progress *TrackMealProgressService
	goals    MacroGoalLookup
}

func NewGoalRecommendationService(progress *TrackMealProgressService, goals MacroGoalLookup) *GoalRecommendationService {
	return &GoalRecommendationService{progress: progress, goals: goals}
}

// Recommendation is a deterministic, rule-based suggestion — not an LLM
// call. Application logic stays out of the AI layer per CLAUDE.md; the coach
// can still talk about this in its own words, but the decision of what to
// suggest is made here, reproducibly.
type Recommendation struct {
	Message              string
	SuggestedCalorieGoal float64
	SuggestedSplit       string
	Reason               string
}

// Recommend looks at the trailing two weeks of logged intake against the
// current macro goal and suggests one of: log more consistently, tighten a
// cutting deficit that is not holding, raise a bulking target that is
// under-eaten, or stay the course.
func (s *GoalRecommendationService) Recommend(ctx context.Context, userID uuid.UUID) (Recommendation, error) {
	adherence, err := s.progress.AdherenceScore(ctx, userID, recommendationWindowDays)
	if err != nil {
		return Recommendation{}, err
	}

	if adherence < adherenceFloor {
		return Recommendation{
			Message: "Logging has been inconsistent the last couple of weeks — try logging every meal for a few days before adjusting targets.",
			Reason:  "low adherence",
		}, nil
	}

	goal, err := s.goals.Current(ctx, userID)
	if err != nil {
		return Recommendation{}, err
	}

	avgDelta, err := s.averageDelta(ctx, userID, recommendationWindowDays)
	if err != nil {
		return Recommendation{}, err
	}

	switch goal.Goal {
	case calculator.GoalCutting:
		if avgDelta > driftThresholdKcal {
			return Recommendation{
				Message:              "You're averaging over your cutting target — consider tightening portions or your macro split.",
				SuggestedCalorieGoal: goal.CalorieGoal,
				Reason:               "trailing average over cutting goal",
			}, nil
		}
	case calculator.GoalBulking:
		if avgDelta < -driftThresholdKcal {
			return Recommendation{
				Message:              "You're averaging under your bulking target — consider raising your calorie goal or adding a snack.",
				SuggestedCalorieGoal: goal.CalorieGoal,
				Reason:               "trailing average under bulking goal",
			}, nil
		}
	default:
		if avgDelta > driftThresholdKcal || avgDelta < -driftThresholdKcal {
			return Recommendation{
				Message: "Your logged intake has been drifting from your maintenance target — worth a look next time you plan meals.",
				Reason:  "trailing average off maintenance goal",
			}, nil
		}
	}

	return Recommendation{Message: "You're on track — no changes needed.", Reason: "within tolerance"}, nil
}

func (s *GoalRecommendationService) averageDelta(ctx context.Context, userID uuid.UUID, days int) (float64, error) {
	to := time.Now()
	from := to.AddDate(0, 0, -(days - 1))

	progress, err := s.progress.ForRange(ctx, userID, from, to)
	if err != nil {
		return 0, err
	}
	if len(progress) == 0 {
		return 0, nil
	}

	var total float64
	for _, p := range progress {
		total += p.DeltaCalories
	}
	return total / float64(len(progress)), nil
}
