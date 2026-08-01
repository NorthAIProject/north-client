package workouts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
)

// generationAttempts is how many times a plan may be generated.
//
// One retry, not a loop. If the model breaks a stated constraint twice in a row
// with the violation spelled out for it, the problem is the prompt or the
// model, and grinding through five attempts costs the user a minute of waiting
// to arrive at the same answer.
const generationAttempts = 2

// planTemperature is low. Plan generation is a constraint-satisfaction task,
// not a creative one; the person's equipment list is not a suggestion.
var planTemperature = float32(0.2)

type Service struct {
	repo     *Repository
	registry *ai.Registry
	model    string
}

type Options struct {
	Repository *Repository
	Registry   *ai.Registry
	Model      string
}

func NewService(opts Options) *Service {
	return &Service{repo: opts.Repository, registry: opts.Registry, model: opts.Model}
}

// ValidateIntake checks the form before anything is generated.
func ValidateIntake(in Intake) error {
	var errs apperr.FieldErrors

	if strings.TrimSpace(in.Goal) == "" {
		errs = errs.Add("goal", "Tell North what you are training for.")
	} else if len(in.Goal) > 500 {
		errs = errs.Add("goal", "Keep this under 500 characters.")
	}

	if in.DaysPerWeek < 1 || in.DaysPerWeek > 7 {
		errs = errs.Add("days_per_week", "Choose between 1 and 7 days.")
	}
	if in.SessionMinutes < 10 || in.SessionMinutes > 240 {
		errs = errs.Add("session_minutes", "Choose a session length between 10 and 240 minutes.")
	}
	if len(in.Limitations) > 1000 {
		errs = errs.Add("limitations", "Keep this under 1000 characters.")
	}

	return errs.OrNil()
}

// Generate produces a plan that satisfies the intake. It does not persist:
// storing is the caller's job, so there is exactly one write and one row.
//
// A plan that breaks the person's stated constraints is never returned. It goes
// back to the model with the specific violations, and only a plan that passes
// comes out. Handing back a plan the user cannot follow would be worse than
// failing, because they would try to follow it.
func (s *Service) Generate(ctx context.Context, user users.User, in Intake) (Plan, string, error) {
	if err := ValidateIntake(in); err != nil {
		return Plan{}, "", err
	}

	log := middleware.FromContext(ctx)

	client, err := s.registry.Default()
	if err != nil {
		return Plan{}, "", err
	}

	system, err := prompts.Raw(prompts.WorkoutPlan)
	if err != nil {
		return Plan{}, "", err
	}
	system += "\n\n## CONTEXT\n\n" + intakeContext(user, in)

	messages := []ai.Message{ai.UserText(intakeRequest(in))}

	var lastProblems []string

	for attempt := 1; attempt <= generationAttempts; attempt++ {
		resp, err := client.Generate(ctx, ai.Request{
			Model:          s.model,
			System:         system,
			Messages:       messages,
			ResponseSchema: PlanSchema(),
			Temperature:    &planTemperature,
		})
		if err != nil {
			return Plan{}, "", apperr.Wrap(err, "generate plan")
		}

		var plan Plan
		if err := json.Unmarshal([]byte(resp.Text), &plan); err != nil {
			lastProblems = []string{"the reply was not valid JSON for the required shape"}
			log.Warn("plan did not decode", slog.Int("attempt", attempt), slog.Any("error", err))
			messages = append(messages,
				ai.ModelText(resp.Text),
				ai.UserText("That was not valid JSON matching the schema. Return the plan again, correctly."),
			)
			continue
		}

		problems := Validate(plan, in)
		if len(problems) == 0 {
			return plan, client.Name(), nil
		}

		lastProblems = problems
		log.Warn("generated plan broke the intake constraints",
			slog.Int("attempt", attempt),
			slog.Any("problems", problems))

		// Naming the specific violation is what makes the retry work. "Try
		// again" produces the same plan; "you used a barbell and they only have
		// dumbbells" produces a correct one.
		messages = append(messages,
			ai.ModelText(resp.Text),
			ai.UserText("That plan breaks the constraints:\n- "+strings.Join(problems, "\n- ")+
				"\n\nReturn a corrected plan that satisfies every constraint."),
		)
	}

	return Plan{}, "", apperr.Wrap(apperr.ErrUnavailable,
		"could not produce a plan that fits those constraints: %s", strings.Join(lastProblems, "; "))
}

// CreatePlan records an intake, generates a plan against it, and stores both.
// This is what the handler calls.
//
// The intake is written first and unconditionally, even though generation may
// fail. What the person told us about their training is worth keeping on its
// own: it pre-fills the form on their next attempt rather than making them
// answer the same questions again.
func (s *Service) CreatePlan(ctx context.Context, user users.User, in Intake) (StoredPlan, error) {
	if err := ValidateIntake(in); err != nil {
		return StoredPlan{}, err
	}

	intake, err := s.repo.CreateIntake(ctx, user.ID, in)
	if err != nil {
		return StoredPlan{}, err
	}

	plan, provider, err := s.Generate(ctx, user, in)
	if err != nil {
		return StoredPlan{}, err
	}

	return s.repo.CreatePlan(ctx, StoredPlan{
		UserID:   user.ID,
		IntakeID: intake.ID,
		Plan:     plan,
		Model:    s.model,
		Provider: provider,
	})
}

func (s *Service) GetPlan(ctx context.Context, id, userID uuid.UUID) (StoredPlan, error) {
	return s.repo.GetPlan(ctx, id, userID)
}

func (s *Service) LatestPlan(ctx context.Context, userID uuid.UUID) (StoredPlan, error) {
	return s.repo.LatestPlan(ctx, userID)
}

func (s *Service) LatestIntake(ctx context.Context, userID uuid.UUID) (StoredIntake, error) {
	return s.repo.LatestIntake(ctx, userID)
}

func (s *Service) ListPlans(ctx context.Context, userID uuid.UUID, limit int) ([]StoredPlan, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.repo.ListPlans(ctx, userID, limit)
}

func intakeContext(user users.User, in Intake) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Name: %s\n", user.DisplayName)
	fmt.Fprintf(&b, "Experience: %s\n", orUnknown(in.Experience))
	if style := strings.TrimSpace(user.CoachingStyle); style != "" {
		fmt.Fprintf(&b, "How they want to be coached: %s\n", style)
	}

	return b.String()
}

// intakeRequest states the constraints in the user turn, where models weight
// instructions most heavily.
func intakeRequest(in Intake) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Goal: %s\n", in.Goal)
	fmt.Fprintf(&b, "I can train exactly %d days a week.\n", in.DaysPerWeek)
	fmt.Fprintf(&b, "Each session can be at most %d minutes, including warm-up.\n", in.SessionMinutes)

	if len(in.Equipment) == 0 {
		b.WriteString("I have no equipment at all. Bodyweight only.\n")
	} else {
		fmt.Fprintf(&b, "The only equipment I have is: %s. Nothing else.\n", strings.Join(in.Equipment, ", "))
	}

	if limits := strings.TrimSpace(in.Limitations); limits != "" {
		fmt.Fprintf(&b, "Injuries or limitations to work around: %s\n", limits)
	}

	return b.String()
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "not stated"
	}
	return s
}
