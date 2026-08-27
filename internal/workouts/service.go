package workouts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/spend"
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

	// SearchByName finds exercises by name, for the swap panel's picker. Kept
	// narrower than the browse page's filter so this package kept depending on
	// "somewhere to look exercises up" rather than on the exercises slice.
	SearchByName(ctx context.Context, query string, equipment []string, limit int) ([]exercise.Exercise, error)
}

type Service struct {
	repo    *Repository
	runner  *ai.Runner
	catalog Catalog
	model   string
}

type Options struct {
	Repository *Repository
	Runner     *ai.Runner

	// Catalog may be nil, which turns the candidate list off and leaves the
	// model to name exercises freely — the behaviour before the catalog
	// existed. Tests that care about generation but not about the catalog do
	// not have to build one.
	Catalog Catalog

	Model string
}

func NewService(opts Options) *Service {
	return &Service{
		repo:    opts.Repository,
		runner:  opts.Runner,
		catalog: opts.Catalog,
		model:   opts.Model,
	}
}

// ValidateIntake checks the form before anything is generated.
func ValidateIntake(in Intake) error {
	var errs apperr.FieldErrors

	if strings.TrimSpace(in.Goal) == "" {
		errs = errs.Add("goal", "Tell Khepri what you are training for.")
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

	var (
		plan         Plan
		lastProblems []string
	)

	// A plan can take up to three generations per provider, each carrying the
	// rejected drafts before it, so this is the most expensive single request
	// in the product and was entirely unrecorded.
	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfaceWorkoutPlan)

	client, err := s.runner.Run(ctx, ai.RunOptions{Tier: string(user.Tier)}, func(client ai.Client) error {
		// Each provider opens its own correction dialogue. Carrying another
		// model's rejected drafts across would ask this one to repair words it
		// never wrote, and the correction only works because it quotes the
		// exact plan that broke.
		messages := []ai.Message{ai.UserText(intakeRequest(in))}
		lastProblems = nil

		for attempt := 1; attempt <= generationAttempts; attempt++ {
			resp, genErr := client.Generate(ctx, ai.Request{
				Model:          s.model,
				System:         system,
				Messages:       messages,
				ResponseSchema: PlanSchema(),
				Temperature:    &planTemperature,
			})
			if genErr != nil {
				return apperr.Wrap(genErr, "generate plan")
			}

			var candidate Plan
			if decErr := json.Unmarshal([]byte(resp.Text), &candidate); decErr != nil {
				lastProblems = []string{"the reply was not valid JSON for the required shape"}
				log.Warn("plan did not decode", slog.Int("attempt", attempt), slog.Any("error", decErr))
				messages = append(messages,
					ai.ModelText(resp.Text),
					ai.UserText("That was not valid JSON matching the schema. Return the plan again, correctly."),
				)
				continue
			}

			problems := Validate(candidate, in)
			if len(problems) == 0 {
				plan = candidate
				return nil
			}

			lastProblems = problems
			log.Warn("generated plan broke the intake constraints",
				slog.Int("attempt", attempt),
				slog.Any("problems", problems))

			// Naming the specific violation is what makes the retry work. "Try
			// again" produces the same plan; "you used a barbell and they only
			// have dumbbells" produces a correct one.
			messages = append(messages,
				ai.ModelText(resp.Text),
				ai.UserText("That plan breaks the constraints:\n- "+strings.Join(problems, "\n- ")+
					"\n\nReturn a corrected plan that satisfies every constraint."),
			)
		}

		// ErrUnavailable rather than a plain error so the chain moves on: a
		// model that broke the constraints twice with the violations spelled
		// out is unlikely to succeed on a third pass, and a different model
		// might on its first. If every provider fails the same way the user
		// sees this same message, just later.
		return apperr.Wrap(apperr.ErrUnavailable,
			"could not produce a plan that fits those constraints: %s", strings.Join(lastProblems, "; "))
	})
	if err != nil {
		return Plan{}, "", err
	}

	s.applyCatalog(ctx, &plan)
	return plan, client.Name(), nil
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

// ErrPlanSuperseded means the plan an edit was made against is no longer the
// user's newest.
//
// Two tabs, or a double-submitted button, would otherwise each fork from the
// same parent and one edit would silently disappear. Editing is append-only, so
// nothing is corrupted — but a fork means the change someone just watched
// happen is not the plan they are following, which is worse than being told to
// look again.
var ErrPlanSuperseded = apperr.New("this plan has been edited since it was loaded")

// SwapExercise replaces one movement in a plan, keeping its prescription.
func (s *Service) SwapExercise(ctx context.Context, user users.User, planID uuid.UUID, day, index int, catalogSlug string) (StoredPlan, error) {
	replacement, err := s.movement(ctx, catalogSlug)
	if err != nil {
		return StoredPlan{}, err
	}

	return s.applyEdit(ctx, user, planID, func(p Plan) (Plan, error) {
		return Swap(p, day, index, replacement)
	})
}

// AddExercise appends a catalog exercise to the end of a day.
//
// Appended rather than inserted at a chosen position: order matters in training
// — compounds before accessories — but choosing where is what reordering is
// for, and building both at once would mean two half-designed controls instead
// of one finished each.
func (s *Service) AddExercise(ctx context.Context, user users.User, planID uuid.UUID, day int, catalogSlug string) (StoredPlan, error) {
	movement, err := s.movement(ctx, catalogSlug)
	if err != nil {
		return StoredPlan{}, err
	}

	return s.applyEdit(ctx, user, planID, func(p Plan) (Plan, error) {
		if day < 0 || day >= len(p.Days) {
			return Plan{}, fmt.Errorf("day %d is outside this plan's %d days", day, len(p.Days))
		}
		return Insert(p, day, len(p.Days[day].Exercises), NewExercise(movement))
	})
}

// RemoveExercise drops one exercise from a day.
func (s *Service) RemoveExercise(ctx context.Context, user users.User, planID uuid.UUID, day, index int) (StoredPlan, error) {
	return s.applyEdit(ctx, user, planID, func(p Plan) (Plan, error) {
		return Remove(p, day, index)
	})
}

// MoveExercise reorders one exercise within its day.
func (s *Service) MoveExercise(ctx context.Context, user users.User, planID uuid.UUID, day, from, to int) (StoredPlan, error) {
	return s.applyEdit(ctx, user, planID, func(p Plan) (Plan, error) {
		return Move(p, day, from, to)
	})
}

// SetPrescription changes how much of an exercise to do, leaving the movement
// alone.
func (s *Service) SetPrescription(ctx context.Context, user users.User, planID uuid.UUID, day, index, sets int, reps string, restSeconds int) (StoredPlan, error) {
	return s.applyEdit(ctx, user, planID, func(p Plan) (Plan, error) {
		return SetPrescription(p, day, index, sets, reps, restSeconds)
	})
}

// SuggestForDay offers exercises to add to a day.
//
// Unlike a swap there is no movement to match against, so the useful filter is
// simply "things you can do that are not already in this session" — repeating
// an exercise the day already contains is the one suggestion that is certainly
// wrong.
func (s *Service) SuggestForDay(ctx context.Context, user users.User, planID uuid.UUID, day int) ([]exercise.Exercise, error) {
	stored, err := s.repo.GetPlan(ctx, planID, user.ID)
	if err != nil {
		return nil, err
	}
	if day < 0 || day >= len(stored.Plan.Days) {
		return nil, apperr.ErrValidation
	}

	already := map[string]bool{}
	for _, ex := range stored.Plan.Days[day].Exercises {
		if ex.CatalogSlug != "" {
			already[ex.CatalogSlug] = true
		}
	}

	var equipment []string
	if intake, err := s.repo.LatestIntake(ctx, user.ID); err == nil {
		equipment = intake.Intake.Equipment
	}

	out := make([]exercise.Exercise, 0, suggestionCount)
	for _, row := range s.candidates(ctx, Intake{Equipment: equipment}) {
		if len(out) == suggestionCount {
			break
		}
		if already[row.Slug] {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// suggestionCount is how many replacements the swap panel pins above the full
// catalog. Short enough to scan without reading; the picker is right below it
// for anything else.
const suggestionCount = 5

// SuggestReplacements ranks catalog exercises that could stand in for the one
// at day/index.
//
// Deliberately not a model call. The catalog carries primary_muscles,
// secondary_muscles and equipment as structured columns, so "trains the same
// thing, with gear they have" is arithmetic — and this runs every time the swap
// panel opens, on a screen that should feel instant. North meters every model
// call precisely so that spending one is a decision; revisit only if the
// ordering proves insufficient in use.
func (s *Service) SuggestReplacements(ctx context.Context, user users.User, planID uuid.UUID, day, index int) ([]exercise.Exercise, error) {
	stored, err := s.repo.GetPlan(ctx, planID, user.ID)
	if err != nil {
		return nil, err
	}
	if day < 0 || day >= len(stored.Plan.Days) {
		return nil, apperr.ErrValidation
	}
	if index < 0 || index >= len(stored.Plan.Days[day].Exercises) {
		return nil, apperr.ErrValidation
	}
	current := stored.Plan.Days[day].Exercises[index]

	// The intake's equipment, so a suggestion is something they can actually
	// perform. A missing intake is not fatal: an unfiltered list of movements
	// that train the right muscle still beats no suggestions.
	var equipment []string
	if intake, err := s.repo.LatestIntake(ctx, user.ID); err == nil {
		equipment = intake.Intake.Equipment
	}

	candidates := s.candidates(ctx, Intake{Equipment: equipment})

	type scored struct {
		row   exercise.Exercise
		score int
	}
	var ranked []scored
	for _, row := range candidates {
		// The lift already in the plan is not a replacement for itself.
		if row.Slug == current.CatalogSlug {
			continue
		}
		score := 2*overlap(row.Primary, current.Primary) + overlap(row.Secondary, current.Secondary)
		if score == 0 {
			continue
		}
		ranked = append(ranked, scored{row: row, score: score})
	}

	// Score first, then name, so the same plan always offers the same order —
	// a list that reshuffles between opens is one nobody learns to trust.
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].row.Name < ranked[j].row.Name
	})

	out := make([]exercise.Exercise, 0, suggestionCount)
	for _, candidate := range ranked {
		if len(out) == suggestionCount {
			break
		}
		out = append(out, candidate.row)
	}
	return out, nil
}

// overlap counts the muscle keys two lists share.
func overlap(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, key := range a {
		set[key] = true
	}

	count := 0
	for _, key := range b {
		if set[key] {
			count++
		}
	}
	return count
}

// SearchCatalog backs the swap panel's picker: anything in the catalog matching
// what was typed, narrowed to equipment the intake recorded.
//
// Narrowed rather than unrestricted because the panel exists to answer "what
// else could I do here", and offering a machine to someone training at home is
// not an answer. Someone who wants the whole catalog has the browse page.
func (s *Service) SearchCatalog(ctx context.Context, user users.User, query string) ([]exercise.Exercise, error) {
	if s.catalog == nil {
		return nil, nil
	}

	var equipment []string
	if intake, err := s.repo.LatestIntake(ctx, user.ID); err == nil {
		equipment = withBodyweight(intake.Intake.Equipment)
	}

	found, err := s.catalog.SearchByName(ctx, query, equipment, suggestionCount*4)
	if err != nil {
		return nil, apperr.Wrap(err, "search the catalog")
	}
	return found, nil
}

// withBodyweight adds "none" to an equipment list, so a search narrowed to what
// someone owns still offers movements that need nothing at all.
//
// exercises.Service.Candidates does this for the generator already; SearchByName
// goes through the browse filter instead, where an equipment list is a strict
// match and a push-up would be filtered out for someone who owns dumbbells.
//
// An empty list stays empty: there it means "no constraint", and turning it
// into {"none"} would narrow an unknown intake all the way down to bodyweight.
func withBodyweight(equipment []string) []string {
	if len(equipment) == 0 {
		return nil
	}
	for _, item := range equipment {
		if item == exercise.EquipmentNone {
			return equipment
		}
	}
	return append([]string{exercise.EquipmentNone}, equipment...)
}

// movement resolves a catalog slug into the fields a swap writes.
//
// The slug arrives from a form, so an unknown one is a bad request rather than
// a server fault — and must never be written through as a plan exercise naming
// a catalog row that does not exist.
func (s *Service) movement(ctx context.Context, slug string) (Movement, error) {
	if s.catalog == nil {
		return Movement{}, apperr.ErrUnavailable
	}

	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return Movement{}, apperr.ErrValidation
	}

	found, err := s.catalog.Resolve(ctx, []string{slug})
	if err != nil {
		return Movement{}, apperr.Wrap(err, "resolve replacement exercise")
	}
	row, ok := found[slug]
	if !ok {
		return Movement{}, apperr.ErrNotFound
	}

	return Movement{
		Name:             row.Name,
		Equipment:        row.Equipment,
		CatalogSlug:      row.Slug,
		IllustrationSlug: row.IllustrationSlug,
		Primary:          row.Primary,
		Secondary:        row.Secondary,
	}, nil
}

// applyEdit is the one path every plan edit takes: load, apply, store as a new
// row.
//
// A new row rather than an UPDATE. That keeps the model's original readable,
// keeps intake_id and the generation columns satisfiable without inventing an
// intake, and means /app/training — which already resolves to the newest plan —
// shows the edit with no routing change. See migrations/20260827190000.
//
// Validation runs but does not block. The intake describes a typical week
// rather than a contract, and someone who answered "dumbbells" may be standing
// in a hotel gym today; refusing their edit because a form said otherwise is
// the software being more confident than the person. The generation path keeps
// blocking, where a broken constraint is a model defect rather than a choice.
// Callers surface Plan.Problems as a notice.
func (s *Service) applyEdit(ctx context.Context, user users.User, planID uuid.UUID, edit func(Plan) (Plan, error)) (StoredPlan, error) {
	current, err := s.repo.GetPlan(ctx, planID, user.ID)
	if err != nil {
		return StoredPlan{}, err
	}

	// Scoped to this plan, not the account. Generating a second plan is not a
	// conflict with editing the first — the race worth catching is two tabs on
	// the same plan. Comparing against the account's newest row instead made
	// every older plan permanently uneditable.
	newest, err := s.repo.LatestPlanForIntake(ctx, user.ID, current.IntakeID)
	if err != nil {
		return StoredPlan{}, err
	}
	if newest.ID != current.ID {
		return StoredPlan{}, ErrPlanSuperseded
	}

	edited, err := edit(current.Plan)
	if err != nil {
		// The day and exercise indices come from a URL, so out of range is a
		// bad request rather than a server fault.
		return StoredPlan{}, apperr.Wrap(apperr.ErrValidation, "%v", err)
	}

	return s.repo.CreatePlan(ctx, StoredPlan{
		UserID:   user.ID,
		IntakeID: current.IntakeID,
		Plan:     edited,

		// Carried from the plan this descends from: they record which
		// generation it came out of, which stays true after an edit. Source is
		// what says a person touched it.
		Model:      current.Model,
		Provider:   current.Provider,
		Source:     SourceEdited,
		EditedFrom: &current.ID,
	})
}

func (s *Service) GetPlan(ctx context.Context, id, userID uuid.UUID) (StoredPlan, error) {
	return s.repo.GetPlan(ctx, id, userID)
}

// PlanForDisplay is GetPlan with the catalog re-applied, for the page that
// renders a plan's exercises.
//
// Separate from GetPlan because the catalog lookup is a query nobody else
// needs: the dashboard only wants the next session's name, and the coach's
// context only wants Plan.Summary(). Putting it in GetPlan would add a query to
// both.
//
// Re-applying on read rather than trusting what was stored is what gives older
// plans their artwork. applyCatalog runs at generation, so a plan built before
// the catalog carried illustrations has no illustration_slug in its JSONB and
// never would. It also keeps the catalog the single source of truth: a
// corrected muscle or a newly added illustration reaches plans that already
// exist.
func (s *Service) PlanForDisplay(ctx context.Context, id, userID uuid.UUID) (StoredPlan, []string, error) {
	stored, err := s.repo.GetPlan(ctx, id, userID)
	if err != nil {
		return StoredPlan{}, nil, err
	}
	s.applyCatalog(ctx, &stored.Plan)

	// What the plan no longer satisfies about the intake it was built from.
	// Reported, never enforced — an edit that breaks a stated constraint is
	// usually someone changing their mind. A missing intake is not worth
	// failing the page over; it just means nothing to compare against.
	var problems []string
	if intake, err := s.repo.GetIntake(ctx, stored.IntakeID, userID); err == nil {
		problems = Validate(stored.Plan, intake.Intake)
	}

	return stored, problems, nil
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

// ListPlans returns every stored plan row, newest first — which since editing
// means every *version* of every plan. handler.index uses it to answer "is
// there anything at all"; anything showing plans to a person wants
// ListCurrentPlans instead.
func (s *Service) ListPlans(ctx context.Context, userID uuid.UUID, limit int) ([]StoredPlan, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.repo.ListPlans(ctx, userID, limit)
}

// CurrentVersionOf resolves the newest version of the plan a stale request
// named, so a refused edit can re-render what is actually there.
func (s *Service) CurrentVersionOf(ctx context.Context, user users.User, planID uuid.UUID) (StoredPlan, error) {
	stale, err := s.repo.GetPlan(ctx, planID, user.ID)
	if err != nil {
		return StoredPlan{}, err
	}
	return s.repo.LatestPlanForIntake(ctx, user.ID, stale.IntakeID)
}

// ListCurrentPlans returns one row per plan: the version the person is
// currently following, most recently touched first.
//
// The distinction from ListPlans is invisible in the names alone, which is why
// both carry it in a comment. A plan is an intake — every edit of it shares
// that intake_id — so this collapses a plan's edit history down to the version
// that matters and leaves the rest stored.
func (s *Service) ListCurrentPlans(ctx context.Context, userID uuid.UUID, limit int) ([]StoredPlan, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.repo.ListCurrentPlans(ctx, userID, limit)
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
			ex.IllustrationSlug = catalogued.IllustrationSlug
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
