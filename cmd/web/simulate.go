package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/shared/simulate"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// simulatedSource names the provider on generated health readings, so a real
// device sync can never be confused with an invented one and the rows can be
// removed by source alone.
const simulatedSource = "simulated"

// simulatedWeightKg is the body weight the MET calorie estimate is computed
// against.
//
// A constant because biometrics are not simulated: nothing here writes a
// weight, height or date of birth, so the calculator and biometrics context
// sources stay empty for a simulated account. Any detector that needs a real
// body has to wait for that. This one number exists only so activity sessions
// get a plausible calorie figure instead of zero.
const simulatedWeightKg = 78.0

// runSimulate invents accounts with months of behaviour behind them.
//
// A subcommand rather than a route for the same reason `tier` and `spend` are:
// this describes development, not a user's account, and an endpoint that can
// manufacture accounts is a surface nobody wants to have to secure.
//
// It exists because the pattern detectors, the nudge-effectiveness loop and the
// commitment resolver all learn from months of behaviour, and the product has
// none. Tuning them against the single real account available means tuning
// against a sample of one with no way to see the overfit.
func runSimulate(args []string) error {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	count := fs.Int("users", 40, "how many accounts to invent")
	weeks := fs.Int("weeks", 16, "weeks of history per account (minimum 6)")
	seed := fs.Uint64("seed", 7, "makes a run reproducible")
	personas := fs.String("persona", "", "comma-separated persona names; empty means the whole catalog")
	dryRun := fs.Bool("dry-run", false, "generate and summarise without writing anything")
	purge := fs.Bool("purge", false, "delete previously simulated accounts first")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: main simulate [flags]\n\nflags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\npersonas:\n")
		for _, p := range simulate.Personas {
			fmt.Fprintf(os.Stderr, "  %-22s %s\n", p.Name, p.Finding)
		}
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	var only []string
	if trimmed := strings.TrimSpace(*personas); trimmed != "" {
		only = strings.Split(trimmed, ",")
		for i := range only {
			only[i] = strings.TrimSpace(only[i])
		}
	}

	people, err := simulate.Generate(simulate.Options{
		Users: *count,
		Weeks: *weeks,
		Seed:  *seed,
		Only:  only,
	})
	if err != nil {
		return err
	}

	if *dryRun {
		summarise(people)
		return nil
	}

	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(newLogger(cfg))

	// The refusal is here rather than in the simulate package because the
	// package has no opinion about environments and should stay testable
	// without configuration. This is the one place that knows.
	if cfg.Env.IsProduction() {
		return errors.New("refusing to write simulated accounts to a production environment")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if *purge {
		removed, err := purgeSimulated(ctx, pool)
		if err != nil {
			return fmt.Errorf("purge simulated accounts: %w", err)
		}
		fmt.Printf("removed %d previously simulated account(s)\n", removed)
	}

	w := &simWriter{
		users:         users.NewRepository(pool),
		habits:        habits.NewRepository(pool),
		checkins:      checkins.NewRepository(pool),
		sleep:         sleep.NewRepository(pool),
		hydration:     hydration.NewRepository(pool),
		health:        health.NewRepository(pool),
		nudges:        nudges.NewRepository(pool),
		workouts:      workouts.NewRepository(pool),
		activity:      activity.NewRepository(pool),
		meals:         meals.NewRepository(pool),
		conversations: conversations.NewRepository(pool),
		pool:          pool,
	}

	if err := w.resolveIngredients(ctx); err != nil {
		return err
	}

	for i, person := range people {
		if err := w.write(ctx, person); err != nil {
			return fmt.Errorf("write %s (%s): %w", person.Email, person.Persona.Name, err)
		}
		// Progress on one line per account: a sixteen-week run of forty people
		// writes tens of thousands of rows and silence looks like a hang.
		fmt.Printf("[%2d/%2d] %-26s %-20s %d days\n",
			i+1, len(people), person.Persona.Name, person.Timezone, len(person.Days))
	}

	fmt.Printf("\nwrote %d simulated account(s), %d weeks each, seed %d\n", len(people), *weeks, *seed)
	fmt.Printf("remove them again with: main simulate --purge --dry-run=false --users %d\n", *count)
	return nil
}

// summarise prints what a run would write, per persona, without a database.
// Useful for checking a persona's shape after editing its parameters.
func summarise(people []simulate.Person) {
	fmt.Printf("%d simulated account(s), %d days each\n\n", len(people), len(people[0].Days))

	for _, p := range people {
		var checkIns, sessions, foodDays, turns, nudgeCount, read, acted int
		for _, d := range p.Days {
			if d.CheckIn != nil {
				checkIns++
			}
			if d.WorkoutDone {
				sessions++
			}
			if len(d.FoodLog) > 0 {
				foodDays++
			}
			turns += len(d.Messages)
			for _, n := range d.Nudges {
				nudgeCount++
				if n.Read {
					read++
				}
				if n.Acted {
					acted++
				}
			}
		}

		// Sessions are shown against the plan they were measured against,
		// because the pair is the finding and either number alone is not.
		weeks := float64(p.Weeks())
		fmt.Printf("%-22s %-20s check-ins %3d  sessions %5.1f/wk of %d planned  food %3d days  turns %3d  nudges %3d (read %3d, acted %3d)\n",
			p.Persona.Name, p.Timezone, checkIns,
			float64(sessions)/weeks, p.TrainingPlan.DaysPerWeek,
			foodDays, turns, nudgeCount, read, acted)
	}
}

// purgeSimulated removes accounts on the reserved simulated domain.
//
// Raw SQL, scoped to a domain no real signup can ever use, rather than the
// account erasure service: erasure also reaches into object storage and the
// spend ledger to satisfy a deletion request, which is the right behaviour for
// a person and unnecessary work for an invented one. Every table hangs off
// users with ON DELETE CASCADE, so the delete is complete.
func purgeSimulated(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx,
		`DELETE FROM users WHERE email LIKE '%@' || $1`, simulate.SimulatedEmailDomain)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// simWriter holds the repositories one person's history is written through.
//
// Repositories rather than services, because every service dates its writes
// from time.Now() — checkins.UpsertToday is the clearest case — and a history
// needs to land on the days it happened. Repositories take the date explicitly,
// which is exactly the seam this needs, and they still carry the constraints,
// natural keys and timezone handling that raw SQL would bypass.
type simWriter struct {
	users         *users.Repository
	habits        *habits.Repository
	checkins      *checkins.Repository
	sleep         *sleep.Repository
	hydration     *hydration.Repository
	health        *health.Repository
	nudges        *nudges.Repository
	workouts      *workouts.Repository
	activity      *activity.Repository
	meals         *meals.Repository
	conversations *conversations.Repository
	pool          *pgxpool.Pool

	// ingredients are the catalog rows food logs reference, resolved once.
	ingredients []meal.Ingredient
}

// write creates one account and its whole history.
//
// Not transactional. The repositories take a pool rather than a transaction, so
// wrapping a person's writes would mean threading pgx.Tx through nine slices
// for the benefit of a development tool. The consequence is real and worth
// knowing: a failure partway through leaves a partial account behind, which
// --purge then clears. Anything that reads a simulated account should therefore
// not assume every domain is populated.
func (w *simWriter) write(ctx context.Context, person simulate.Person) error {
	user, err := w.users.Create(ctx, users.NewRecord{
		Email:       person.Email,
		DisplayName: person.DisplayName,
		Timezone:    person.Timezone,
		// No password hash: a simulated account must not be loggable-in.
		// users.Create maps empty to NULL, and every auth path rejects that.
	})
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	// Without this the account is invisible to the sweeps: every one of them
	// walks users.ListOnboarded, so an un-onboarded simulated person would
	// generate no nudges, no reports and no reviews — the opposite of the point.
	if _, err = w.users.MarkOnboarded(ctx, user.ID); err != nil {
		return fmt.Errorf("mark onboarded: %w", err)
	}

	habitIDs, err := w.writeHabits(ctx, user.ID, person)
	if err != nil {
		return err
	}

	if err = w.writeTrainingPlan(ctx, user.ID, person); err != nil {
		return err
	}

	// One conversation per person, not one per day. The coach thread is
	// continuous by design — that is what the rolling summariser compacts and
	// what the memory extractor reads — so a simulated account with sixty
	// one-message threads would exercise neither.
	thread, err := w.conversations.Create(ctx, user.ID, "Coaching", conversations.KindChat)
	if err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}

	for _, day := range person.Days {
		if err := w.writeDay(ctx, user.ID, person, day, habitIDs, thread.ID); err != nil {
			return fmt.Errorf("%s: %w", day.Date.Format(time.DateOnly), err)
		}
	}

	return nil
}

// writeTrainingPlan records what the person signed up for.
//
// An intake and a plan, in that order, because the schema ties a plan to the
// intake it was generated from and a plan with no intake could not be
// regenerated or explained. Marked as coming from the simulator rather than
// from a model: SourceAI would claim a generation that never happened, and plan
// provenance is a column somebody will eventually trust.
func (w *simWriter) writeTrainingPlan(ctx context.Context, userID uuid.UUID, person simulate.Person) error {
	tp := person.TrainingPlan

	intake, err := w.workouts.CreateIntake(ctx, userID, plan.Intake{
		Goal:           "General fitness",
		Experience:     "intermediate",
		DaysPerWeek:    tp.DaysPerWeek,
		SessionMinutes: tp.SessionMinutes,
		Equipment:      []string{"barbell", "dumbbells"},
	})
	if err != nil {
		return fmt.Errorf("create workout intake: %w", err)
	}

	if _, err := w.workouts.CreatePlan(ctx, workouts.StoredPlan{
		UserID:   userID,
		IntakeID: intake.ID,
		Plan:     simulatedPlan(tp),
		Model:    simulatedSource,
		Provider: simulatedSource,
	}); err != nil {
		return fmt.Errorf("create workout plan: %w", err)
	}

	return nil
}

// simulatedPlan builds a plan body with one training day per declared day.
//
// Exercise selection is the same three movements every time, and deliberately
// so: no detector reads which lifts were prescribed, and inventing a varied
// programme would be fiction that looks like data. What matters is that the
// declared day count is real, because that is the number adherence is measured
// against.
func simulatedPlan(tp simulate.TrainingPlan) plan.Plan {
	weekdays := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

	days := make([]plan.PlanDay, 0, tp.DaysPerWeek)
	for i := 0; i < tp.DaysPerWeek && i < len(weekdays); i++ {
		days = append(days, plan.PlanDay{
			Weekday: weekdays[i],
			Focus:   "Full body",
			Exercises: []plan.Exercise{
				{Name: "Back Squat", Sets: 3, Reps: "5", RestSeconds: 150, Equipment: "barbell"},
				{Name: "Bench Press", Sets: 3, Reps: "5", RestSeconds: 150, Equipment: "barbell"},
				{Name: "Row", Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "dumbbells"},
			},
		})
	}

	return plan.Plan{
		Name:       tp.Name,
		Rationale:  "Simulated plan, written by main simulate.",
		WeeksTotal: tp.WeeksTotal,
		Days:       days,
	}
}

func (w *simWriter) writeHabits(ctx context.Context, userID uuid.UUID, person simulate.Person) (map[string]uuid.UUID, error) {
	ids := make(map[string]uuid.UUID, len(person.Habits))
	for _, h := range person.Habits {
		created, err := w.habits.Create(ctx, userID, h.Name, h.Domain, h.Days)
		if err != nil {
			return nil, fmt.Errorf("create habit %q: %w", h.Name, err)
		}
		ids[h.Name] = created.ID
	}
	return ids, nil
}

func (w *simWriter) writeDay(
	ctx context.Context,
	userID uuid.UUID,
	person simulate.Person,
	day simulate.Day,
	habitIDs map[string]uuid.UUID,
	threadID uuid.UUID,
) error {
	for _, name := range day.HabitsKept {
		id, ok := habitIDs[name]
		if !ok {
			return fmt.Errorf("habit %q was kept but never declared", name)
		}
		if err := w.habits.Complete(ctx, id, userID, day.Date); err != nil {
			return fmt.Errorf("complete habit %q: %w", name, err)
		}
	}

	if c := day.CheckIn; c != nil {
		if _, err := w.checkins.Upsert(ctx, userID, checkins.Write{
			LocalDate: day.Date,
			Mood:      c.Mood,
			Energy:    c.Energy,
			Wins:      c.Wins,
			Notes:     c.Notes,
		}); err != nil {
			return fmt.Errorf("check-in: %w", err)
		}
	}

	if s := day.Sleep; s != nil {
		if _, err := w.sleep.Upsert(ctx, userID, day.Date, sleep.Log{
			DurationMinutes: s.DurationMinutes,
			Quality:         s.Quality,
		}); err != nil {
			return fmt.Errorf("sleep log: %w", err)
		}
	}

	if day.HydrationML > 0 {
		if _, err := w.hydration.Create(ctx, userID, day.Date, day.HydrationML); err != nil {
			return fmt.Errorf("hydration: %w", err)
		}
	}

	if readings := deviceReadings(day); len(readings) > 0 {
		if _, err := w.health.Save(ctx, userID, simulatedSource, readings); err != nil {
			return fmt.Errorf("health readings: %w", err)
		}
	}

	if err := w.writeSession(ctx, userID, day); err != nil {
		return err
	}
	if err := w.writeFoodLog(ctx, userID, day); err != nil {
		return err
	}
	if err := w.writeMessages(ctx, threadID, day); err != nil {
		return err
	}

	return w.writeNudges(ctx, userID, day)
}

// deviceReadings turns a day's device numbers into the shape the health ingest
// endpoint would have received from a bridge app.
//
// The device sleep total is written as `sleep_asleep` in minutes. The name is a
// guess, and knowingly so: internal/health says in its own package doc that no
// real payload has ever arrived and that its field names are guesses at what a
// bridge sends. Writing simulated data under a guessed name is fine — writing
// it under a *different* guess from the one the detector reads would not be, so
// the sleep-truth detector must read this same constant.
func deviceReadings(day simulate.Day) []health.Reading {
	var out []health.Reading

	dayEnd := day.Date.AddDate(0, 0, 1)

	if day.DeviceSleepMinutes != nil {
		out = append(out, health.Reading{
			Metric:    health.MetricSleepAsleep,
			Value:     float64(*day.DeviceSleepMinutes),
			Unit:      "min",
			StartedAt: day.Date,
			EndedAt:   &dayEnd,
		})
	}
	if day.Steps > 0 {
		out = append(out, health.Reading{
			Metric:    "steps",
			Value:     float64(day.Steps),
			Unit:      "count",
			StartedAt: day.Date,
			EndedAt:   &dayEnd,
		})
	}
	if day.RestingHR > 0 {
		out = append(out, health.Reading{
			Metric: "resting_heart_rate",
			Value:  float64(day.RestingHR),
			Unit:   "bpm",
			// An instantaneous sample, so no EndedAt: that is the distinction
			// health.Reading draws between a beat and a day's steps.
			StartedAt: day.Date.Add(7 * time.Hour),
		})
	}

	return out
}

// writeNudges inserts a day's nudges, then backdates them.
//
// The backdate is a narrow UPDATE rather than a repository call because
// nudges.Insert defaults created_at to now(), which is correct for the engine
// and wrong for a history: every simulated nudge would arrive today and the
// act-rate aggregate would have no time axis to read.
//
// The `acted` half of the persona's response profile has nowhere to go yet —
// the nudges table records read_at and dismissed_at and nothing about whether
// the nudged thing happened. That column arrives with the nudge-attribution
// work; until then the generated Nudge.Acted is exercised only by the detector
// tests, which read simulate.Person directly. Fabricating a value here would
// mean inventing a schema that does not exist.
func (w *simWriter) writeNudges(ctx context.Context, userID uuid.UUID, day simulate.Day) error {
	for _, n := range day.Nudges {
		created, inserted, err := w.nudges.Insert(ctx, userID, nudges.Draft{
			Kind: n.Kind,
			// Same key the real engine uses: one per kind per local day.
			DedupeKey: day.Date.Format(time.DateOnly),
			Title:     simulatedNudgeTitle(n.Kind),
			Body:      "Simulated nudge.",
		})
		if err != nil {
			return fmt.Errorf("insert %s nudge: %w", n.Kind, err)
		}
		if !inserted {
			continue
		}

		// Mid-morning local time: nudges are raised by an hourly sweep that
		// respects quiet hours, so a plausible hour matters to any detector
		// that reads time of day.
		at := day.Date.Add(9 * time.Hour)

		var readAt *time.Time
		if n.Read {
			// An hour later, which is what makes a response-latency profile
			// possible to compute at all.
			t := at.Add(time.Hour)
			readAt = &t
		}

		if _, err := w.pool.Exec(ctx,
			`UPDATE user_nudges SET created_at = $1, read_at = $2 WHERE id = $3 AND user_id = $4`,
			at, readAt, created.ID, userID,
		); err != nil {
			return fmt.Errorf("backdate %s nudge: %w", n.Kind, err)
		}
	}
	return nil
}

// writeSession records a finished training session.
//
// Import rather than Create-then-Complete: it takes StartedAt and EndedAt
// explicitly, which is exactly what a history needs, and it dedupes on
// (source, external id) so re-running the simulator over the same window does
// not double every session. Create would stamp the session with time.Now() and
// need the same backdating UPDATE the nudges need.
func (w *simWriter) writeSession(ctx context.Context, userID uuid.UUID, day simulate.Day) error {
	if !day.WorkoutDone || day.TrainingMinutes <= 0 {
		return nil
	}

	// Early evening, which is when most people train, and late enough that it
	// cannot cross midnight into the following local day.
	startedAt := day.Date.Add(18 * time.Hour)

	_, inserted, err := w.activity.Import(ctx, activity.ImportInput{
		UserID:       userID,
		ActivityCode: "strength_training",
		Source:       activity.SourceManual,
		// The user id is in the external id because activity_sessions enforces
		// UNIQUE (source, external_id) with no user_id in the index. That is
		// right for Strava, whose activity ids are globally unique, but it means
		// any identifier that is only unique per account collides across
		// accounts — and Import reports a collision as "already imported"
		// rather than as an error.
		ExternalID: simulatedSource + ":" + userID.String() + ":" + day.Date.Format(time.DateOnly),
		StartedAt:  startedAt,
		EndedAt:    startedAt.Add(time.Duration(day.TrainingMinutes) * time.Minute),
		WeightKg:   simulatedWeightKg,
		// No Calories: letting Import fall back to the MET estimate exercises
		// the same path a manually logged session takes.
	})
	if err != nil {
		return fmt.Errorf("import session: %w", err)
	}

	// Within one run every session is new, so a dedupe hit is a bug in the
	// external id rather than a legitimate re-import. Reporting it is what turns
	// a silently sparse history into a message somebody can act on.
	if !inserted {
		return fmt.Errorf("session for %s was deduped against an existing row; the external id is not unique enough",
			day.Date.Format(time.DateOnly))
	}
	return nil
}

// writeFoodLog records what the person said they ate.
//
// Every row references a real catalog ingredient, because food_logs enforces
// CHECK (num_nonnulls(meal_id, ingredient_id) = 1) — there is no such thing as
// a food log entry belonging to neither a meal nor an ingredient. The gram
// weight is worked backwards from the calories the persona was going to eat,
// and the macros come from the ingredient at that weight rather than from a
// guessed split, so the stored row is internally consistent with its own
// reference the way a real one is.
func (w *simWriter) writeFoodLog(ctx context.Context, userID uuid.UUID, day simulate.Day) error {
	for i, entry := range day.FoodLog {
		if len(w.ingredients) == 0 {
			return nil
		}

		// Indexed rather than random: this writer has no random source of its
		// own, and reaching for one would make a seeded run unreproducible.
		ing := w.ingredients[(i+day.Date.YearDay())%len(w.ingredients)]

		grams := gramsForCalories(ing, entry.Kcal)
		macros := ing.MacrosFor(grams)

		if _, err := w.meals.InsertFoodLog(ctx, userID, day.Date,
			nil, &ing.ID, &grams, entry.Label, macros,
		); err != nil {
			return fmt.Errorf("insert food log %q: %w", entry.Label, err)
		}
	}
	return nil
}

// gramsForCalories is the weight of an ingredient that carries a target number
// of calories, bounded to a portion a person would actually eat.
//
// The bound matters: a target of 800 kcal against a low-calorie vegetable would
// otherwise log two kilograms of it, and a simulated day that looks absurd is a
// simulated day nobody trusts.
func gramsForCalories(ing meal.Ingredient, kcal int) float64 {
	per100 := ing.Per100g.Calories
	if per100 <= 0 {
		return 100
	}

	grams := float64(kcal) / (per100 / 100)
	return math.Round(math.Min(math.Max(grams, 20), 600))
}

// simulatedIngredientNames are looked up in the seeded catalog once per run.
//
// Ordinary foods, and ones the seed is likely to contain. Whichever fail to
// resolve are simply skipped: the catalog is seed data that can change, and a
// simulator that refuses to run because "Oats" was renamed would be brittle for
// no benefit.
var simulatedIngredientNames = []string{
	"Chicken breast",
	"White rice",
	"Oats",
	"Egg",
	"Olive oil",
	"Banana",
	"Greek yogurt",
	"Salmon",
}

// resolveIngredients loads the catalog rows the food log will reference.
func (w *simWriter) resolveIngredients(ctx context.Context) error {
	for _, name := range simulatedIngredientNames {
		// uuid.Nil as the user: SearchIngredients returns global rows plus the
		// user's own, and a simulated account has none of its own yet.
		found, err := w.meals.SearchIngredients(ctx, uuid.Nil, name, 1)
		if err != nil {
			return fmt.Errorf("search ingredient %q: %w", name, err)
		}
		if len(found) > 0 {
			w.ingredients = append(w.ingredients, found[0])
		}
	}

	if len(w.ingredients) == 0 {
		return errors.New("no seeded ingredients resolved; food logs cannot reference an ingredient, " +
			"and food_logs requires one — has the ingredient seed migration run?")
	}
	return nil
}

// writeMessages appends a day's coach exchange and backdates it.
//
// Same backdating as the nudges, and for the same reason: Append defaults
// created_at to now(), which is right for a live reply and wrong for a history.
// The hour is the entire signal a response-latency profile reads, so a thread
// where every message arrived this afternoon would be worse than no thread.
func (w *simWriter) writeMessages(ctx context.Context, threadID uuid.UUID, day simulate.Day) error {
	for _, m := range day.Messages {
		role := ai.RoleUser
		if m.FromCoach {
			role = ai.RoleModel
		}

		appended, err := w.conversations.Append(ctx, conversations.NewMessage{
			ConversationID: threadID,
			Role:           role,
			Content:        m.Text,
		})
		if err != nil {
			return fmt.Errorf("append message: %w", err)
		}

		at := day.Date.Add(time.Duration(m.MinuteOfDay) * time.Minute)
		if _, err := w.pool.Exec(ctx,
			`UPDATE messages SET created_at = $1 WHERE id = $2`, at, appended.ID,
		); err != nil {
			return fmt.Errorf("backdate message: %w", err)
		}
	}
	return nil
}

func simulatedNudgeTitle(kind string) string {
	switch kind {
	case "missed_checkin":
		return "How did yesterday go?"
	case "workout_today":
		return "Training today"
	case "briefing_ready":
		return "Your morning briefing"
	default:
		return "Nudge"
	}
}
