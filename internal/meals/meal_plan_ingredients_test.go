package meals_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/meals"
)

// writtenPlan creates a plan in one transaction, which is how every generated
// or imported plan arrives: created_at is the transaction's clock, so all of
// its portions tie and the order they were written in is all that orders them.
// Grams identify a portion, and run down so that they never sort as written.
func writtenPlan(t *testing.T, f planFixture, repo *meals.PostgresRepository, name string, firstGrams float64) (uuid.UUID, map[string][]float64) {
	t.Helper()
	want := map[string][]float64{}
	next := firstGrams
	portions := func(meal string, n int) []meals.NewPortion {
		out := make([]meals.NewPortion, n)
		grams := make([]float64, n)
		for i := range out {
			out[i] = meals.NewPortion{IngredientID: f.chicken.ID, QuantityGrams: next}
			grams[i] = next
			next--
		}
		want[meal] = grams
		return out
	}
	var slots []meals.NewMeal
	for i := range 8 {
		slot := fmt.Sprintf("%s slot %d", name, i)
		alternative := slot + " alternative"
		slots = append(slots, meals.NewMeal{
			Name:         slot,
			Portions:     portions(slot, 20),
			Alternatives: []meals.NewMeal{{Name: alternative, OptionLabel: "B", Portions: portions(alternative, 20)}},
		})
	}
	empty := name + " empty"
	want[empty] = []float64{}
	slots = append(slots, meals.NewMeal{Name: empty})

	id, err := repo.CreatePlan(f.ctx, f.userID, meals.NewPlan{Name: name, Settings: easyMid}, []meals.NewDay{
		{Weekday: time.Monday, Meals: slots[:5]},
		{Weekday: time.Tuesday, Meals: slots[5:]},
	})
	if err != nil {
		t.Fatalf("create plan %s: %v", name, err)
	}
	return id, want
}

// checkIngredients compares every meal and alternative of the plan with what
// was written. A meal with no ingredients must hold an empty slice, not nil:
// the API serialises it, and null is a different answer from [].
func checkIngredients(t *testing.T, label string, plan meals.MealPlan, want map[string][]float64) {
	t.Helper()
	seen := 0
	for _, d := range plan.Days {
		for _, slot := range d.Meals {
			for _, m := range slot.Options() {
				seen++
				if m.Ingredients == nil {
					t.Fatalf("%s: %s has nil ingredients, want an empty slice", label, m.Name)
				}
				got := make([]float64, len(m.Ingredients))
				for i, p := range m.Ingredients {
					got[i] = p.QuantityGrams
					if p.MealID != m.ID || p.IngredientName != "Chicken breast" {
						t.Fatalf("%s: %s holds %+v", label, m.Name, p)
					}
				}
				if !slices.Equal(got, want[m.Name]) {
					t.Fatalf("%s: %s grams = %v, want %v", label, m.Name, got, want[m.Name])
				}
			}
		}
	}
	if seen != len(want) {
		t.Fatalf("%s: saw %d meals, want %d", label, seen, len(want))
	}
}

func TestPlanIngredientsKeepTheOrderTheyWereWrittenIn(t *testing.T) {
	f := newPlanFixture(t, "order@north.test")
	repo := meals.NewRepository(f.pool)
	id, want := writtenPlan(t, f, repo, "first", 5000)
	// A second plan on the same table: nothing of its may reach the first.
	otherID, otherWant := writtenPlan(t, f, repo, "second", 2000)

	plan, err := repo.GetPlan(f.ctx, id, f.userID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	checkIngredients(t, "get", plan, want)
	other, err := repo.GetPlan(f.ctx, otherID, f.userID)
	if err != nil {
		t.Fatalf("get other plan: %v", err)
	}
	checkIngredients(t, "get other", other, otherWant)

	err = repo.WithPlanLocked(f.ctx, id, f.userID, func(tx *meals.PlanTx, _ meals.MealPlan) error {
		locked, planErr := tx.Plan(f.ctx, id, f.userID, true)
		if planErr != nil {
			return planErr
		}
		checkIngredients(t, "locked", locked, want)
		return nil
	})
	if err != nil {
		t.Fatalf("with plan locked: %v", err)
	}
}

// Under the lock the read has to go through the transaction: the changes made
// so far are what the next change is applied to, and none of them may outlive
// a rollback.
func TestPlanReadUnderTheLockSeesTheTransaction(t *testing.T) {
	f := newPlanFixture(t, "locked@north.test")
	repo := meals.NewRepository(f.pool)
	id, want := writtenPlan(t, f, repo, "plan", 5000)
	before, err := repo.GetPlan(f.ctx, id, f.userID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	grown, shrunk := before.Days[0].Meals[0], before.Days[0].Meals[1]

	rollback := errors.New("roll back")
	err = repo.WithPlanLocked(f.ctx, id, f.userID, func(tx *meals.PlanTx, _ meals.MealPlan) error {
		if _, addErr := tx.AddPortions(f.ctx, id, grown.ID, []meals.NewPortion{{IngredientID: f.rice.ID, QuantityGrams: 9999}}); addErr != nil {
			return addErr
		}
		if removeErr := tx.RemovePortion(f.ctx, id, shrunk.ID, shrunk.Ingredients[0].ID); removeErr != nil {
			return removeErr
		}
		seen, planErr := tx.Plan(f.ctx, id, f.userID, true)
		if planErr != nil {
			return planErr
		}
		g := seen.Days[0].Meals[0].Ingredients
		if len(g) != 21 || g[20].QuantityGrams != 9999 || g[20].IngredientName != "White rice" {
			t.Fatalf("added portion not seen under the lock: %d portions, last %+v", len(g), g[len(g)-1])
		}
		s := seen.Days[0].Meals[1].Ingredients
		if len(s) != 19 || s[0].ID != shrunk.Ingredients[1].ID {
			t.Fatalf("removed portion still seen under the lock: %d portions, first %+v", len(s), s[0])
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("with plan locked: %v", err)
	}

	after, err := repo.GetPlan(f.ctx, id, f.userID)
	if err != nil {
		t.Fatalf("get plan after rollback: %v", err)
	}
	checkIngredients(t, "after rollback", after, want)
}

// An edited portion that ties on created_at reads back last, because an update
// writes a new row version. That is not a promise to anyone, only what reading
// each meal on its own always did, and a batched read must not change it.
func TestEditedPortionReadsBackAfterItsTies(t *testing.T) {
	f := newPlanFixture(t, "edited@north.test")
	repo := meals.NewRepository(f.pool)
	id, want := writtenPlan(t, f, repo, "plan", 5000)
	before, err := repo.GetPlan(f.ctx, id, f.userID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	m := before.Days[0].Meals[0]
	const edited = 3
	err = repo.WithPlanLocked(f.ctx, id, f.userID, func(tx *meals.PlanTx, _ meals.MealPlan) error {
		_, setErr := tx.SetPortionGrams(f.ctx, id, m.ID, m.Ingredients[edited].ID, 1234, m.Ingredients[edited].Macros)
		return setErr
	})
	if err != nil {
		t.Fatalf("set grams: %v", err)
	}
	want[m.Name] = append(slices.Delete(want[m.Name], edited, edited+1), 1234)

	after, err := repo.GetPlan(f.ctx, id, f.userID)
	if err != nil {
		t.Fatalf("get plan after edit: %v", err)
	}
	checkIngredients(t, "after edit", after, want)
}

// statementCounter counts the statements that read meal_ingredients.
type statementCounter struct{ reads atomic.Int64 }

func (c *statementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "FROM meal_ingredients") {
		c.reads.Add(1)
	}
	return ctx
}

func (*statementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// A plan is read in one statement however many meals it has: an imported plan
// is a couple of hundred meals, and under the lock every one of those round
// trips was made holding it.
func TestPlanIngredientsAreReadInOneStatement(t *testing.T) {
	f := newPlanFixture(t, "one-statement@north.test")
	id, want := writtenPlan(t, f, meals.NewRepository(f.pool), "plan", 5000)

	counter := &statementCounter{}
	cfg := f.pool.Config()
	cfg.ConnConfig.Tracer = counter
	traced, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatalf("traced pool: %v", err)
	}
	defer traced.Close()
	repo := meals.NewRepository(traced)

	plan, err := repo.GetPlan(f.ctx, id, f.userID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	checkIngredients(t, "traced", plan, want)
	if n := counter.reads.Load(); n != 1 {
		t.Fatalf("GetPlan read meal_ingredients in %d statements, want 1", n)
	}

	counter.reads.Store(0)
	err = repo.WithPlanLocked(f.ctx, id, f.userID, func(tx *meals.PlanTx, _ meals.MealPlan) error {
		_, planErr := tx.Plan(f.ctx, id, f.userID, true)
		return planErr
	})
	if err != nil {
		t.Fatalf("with plan locked: %v", err)
	}
	if n := counter.reads.Load(); n != 1 {
		t.Fatalf("Plan under the lock read meal_ingredients in %d statements, want 1", n)
	}
}
