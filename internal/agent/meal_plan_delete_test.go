package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestDeleteMealPlanRemovesOnlyTheOnePlanNamed(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	register := func(email string) users.User {
		t.Helper()
		u, err := userSvc.Register(ctx, users.Registration{
			Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
			DisplayName: "T", Timezone: "Europe/Lisbon",
		})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	me := register("delete-plan@north.test")
	other := register("delete-plan-other@north.test")

	mealsRepo := meals.NewRepository(pool)
	planSvc := meals.NewMealPlanService(mealsRepo, fixedTarget{ProteinG: 150, FatG: 70, CarbG: 250})
	create := func(userID uuid.UUID, name string) meals.MealPlan {
		t.Helper()
		p, err := planSvc.CreatePlan(ctx, userID, meals.MealPlanInput{
			Name: name, DayCount: 1, Settings: meal.PlanSettings{Type: meal.MidCarb, Mode: meal.Easy},
		}, nil, false)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		return p
	}
	names := func(userID uuid.UUID) []string {
		t.Helper()
		list, err := planSvc.ListPlans(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(list))
		for i, p := range list {
			out[i] = p.Name
		}
		return out
	}

	r := Build(Services{Users: userSvc, Ingredients: meals.NewIngredientService(mealsRepo), MealPlans: planSvc})
	if r.IsReadOnly("delete_meal_plan") {
		t.Fatal("delete_meal_plan is marked read-only, so it would run without an approval card")
	}
	call := func(plan string) ai.ToolResult {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"plan": plan})
		if err != nil {
			t.Fatal(err)
		}
		return r.Invoke(ctx, me.ID, ai.ToolCall{Name: "delete_meal_plan", Arguments: raw})
	}

	create(me.ID, "Cut")
	create(me.ID, "Bulk")
	create(me.ID, "Bulk")
	create(other.ID, "Maintenance")

	// No such plan: their names, nothing deleted.
	res := call("Keto")
	if res.IsError || !strings.Contains(res.Content, `"Cut"`) || !strings.Contains(res.Content, `"Bulk"`) {
		t.Errorf("an unknown plan answered %+v, want their plan names", res)
	}
	if got := names(me.ID); len(got) != 3 {
		t.Fatalf("an unknown name left %v", got)
	}

	// Two plans with the name: asks which, deletes neither.
	res = call("bulk")
	if res.IsError || !strings.Contains(strings.ToLower(res.Content), "which") {
		t.Errorf("two plans called Bulk answered %+v, want a question", res)
	}
	if got := names(me.ID); len(got) != 3 {
		t.Fatalf("an ambiguous name left %v", got)
	}

	// Another person's plan is not theirs to name.
	res = call("Maintenance")
	if strings.Contains(res.Content, "Deleted") {
		t.Errorf("another person's plan was deleted: %+v", res)
	}
	if got := names(other.ID); len(got) != 1 {
		t.Fatalf("the other person's plans are now %v", got)
	}

	// Exactly one match, in any letter case: deleted.
	res = call("  cut ")
	if res.IsError || res.Content != "Deleted Cut." {
		t.Fatalf("deleting Cut answered %+v", res)
	}
	if got := names(me.ID); len(got) != 2 || got[0] != "Bulk" || got[1] != "Bulk" {
		t.Fatalf("after deleting Cut the plans are %v", got)
	}
}
