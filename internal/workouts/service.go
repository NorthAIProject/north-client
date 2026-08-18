package workouts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
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

// Catalog is the slice of the exercise catalog this service needs.
//
// An interface, and a narrow one, so workouts depends on "somewhere to look
// exercises up" rather than on the exercises package — and so a test can
// supply a fixed catalog without a database.
type Catalog interface {
	// Candidates returns exercises performable with this equipment, for the
	// model to choose from.
	Candidates(ctx context.Context, equipment []string) ([]exercise.Exercise, error)

	// Resolve returns the catalog rows for the slugs it recognises. Slugs it
	// does not recognise are simply absent, not an error.
	Resolve(ctx context.Context, slugs []string) (map[string]exercise.Exercise, error)
}

type Service struct {
	repo     *Repository
	registry *ai.Registry
	catalog  Catalog
	model    string
}

type Options struct {
	Repository *Repository
	Registry   *ai.Registry

	// Catalog may be nil, which turns the candidate list off and leaves the
	// model to name exercises freely — the behaviour before the catalog
	// existed. Tests that care about generation but not about the catalog do
	// not have to build one.
	Catalog Catalog

	Model string
}

func NewService(opts Options) *Service {
	return &Service{
		repo:     opts.Repository,
		registry: opts.Registry,
		catalog:  opts.Catalog,
		model:    opts.Model,
	}
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

	// Fetched before the loop so a retry does not pay for it twice, and so a
	// catalog that is unreachable degrades to free-text generation instead of
	// failing the request: a plan named from the model's own vocabulary is
	// worth more than no plan.
	candidates := s.candidates(ctx, in)
	if len(candidates) > 0 {
		system += "\n\n## EXERCISE CATALOG\n\n" + catalogContext(candidates)
	}

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
			s.applyCatalog(ctx, &plan)
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

// DueToday reports whether the latest plan has a session on this local day.
func (s *Service) DueToday(ctx context.Context, user users.User, today time.Time) (string, string, bool, error) {
	stored, err := s.repo.LatestPlan(ctx, user.ID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	session, ok := stored.Plan.NextSession(today)
	if !ok {
		return "", "", false, nil
	}
	if !strings.EqualFold(session.Weekday, today.Weekday().String()) {
		return "", "", false, nil
	}
	title := session.Focus
	if title == "" {
		title = session.Weekday
	}
	return title, "/app/workouts/" + stored.ID.String(), true, nil
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

// candidates fetches the catalog rows the model may pick from.
//
// Failures are logged and swallowed. The catalog improves a plan; it is not
// required to produce one, and a database hiccup should cost the person their
// muscle highlighting, not their programme.
func (s *Service) candidates(ctx context.Context, in Intake) []exercise.Exercise {
	if s.catalog == nil {
		return nil
	}

	found, err := s.catalog.Candidates(ctx, in.Equipment)
	if err != nil {
		middleware.FromContext(ctx).Warn("could not load the exercise catalog; generating without it",
			slog.Any("error", err))
		return nil
	}
	return found
}

func catalogContext(candidates []exercise.Exercise) string {
	var b strings.Builder

	b.WriteString("Prefer exercises from this catalog. Each line is `slug — Name (equipment) [muscles]`.\n")
	b.WriteString("When you use one, copy its slug into catalog_slug exactly. If nothing here fits, use your own exercise and leave catalog_slug empty.\n\n")

	for _, e := range candidates {
		b.WriteString(e.Line())
		b.WriteString("\n")
	}

	return b.String()
}

// applyCatalog replaces the model's muscle keys with the catalog's wherever an
// exercise resolved to a catalog row.
//
// This is the point of the whole catalog: a curated answer to "what does this
// train" beats a generated one. Exercises the model improvised keep their own
// keys, which Validate has already filtered to the canonical set.
//
// A lookup failure leaves the plan exactly as generated, for the same reason
// candidates swallows its errors.
func (s *Service) applyCatalog(ctx context.Context, p *Plan) {
	if s.catalog == nil {
		return
	}

	var slugs []string
	for _, day := range p.Days {
		for _, ex := range day.Exercises {
			if ex.CatalogSlug != "" {
				slugs = append(slugs, ex.CatalogSlug)
			}
		}
	}

	log := middleware.FromContext(ctx)

	var total, matched int
	for _, day := range p.Days {
		total += len(day.Exercises)
	}

	if len(slugs) == 0 {
		log.Info("plan used no catalog exercises", slog.Int("exercises", total))
		return
	}

	found, err := s.catalog.Resolve(ctx, slugs)
	if err != nil {
		log.Warn("could not resolve catalog slugs; keeping the model's muscle keys", slog.Any("error", err))
		return
	}

	for _, day := range p.Days {
		for i := range day.Exercises {
			ex := &day.Exercises[i]
			catalogued, ok := found[strings.ToLower(ex.CatalogSlug)]
			if !ok {
				// A slug the model invented. Blanked so nothing downstream
				// treats it as a real catalog reference.
				ex.CatalogSlug = ""
				continue
			}
			matched++
			ex.Primary = catalogued.Primary
			ex.Secondary = catalogued.Secondary
			// The catalog carries no stabilizers, so the model's stand.
		}
	}

	// The only signal that says whether the catalog is actually being used.
	// Without it, a prompt change that stops the model echoing slugs looks
	// exactly like everything working.
	log.Info("resolved plan exercises against the catalog",
		slog.Int("matched", matched),
		slog.Int("exercises", total))
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
