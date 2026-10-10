package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/meals"
)

// Deleting a meal plan from the chat: "get rid of the old cut plan".
//
// The plan is named, never identified, like every other meal-plan tool. Unlike
// pickMealPlan, only a whole name matches (ignoring case and surrounding
// space): a part of one is fine for reading or editing, where the approval
// card and the result show which plan was meant, but a delete cannot be
// looked at afterwards, and "Cut" must not quietly take "Cut 2" with it on a
// second call.
//
// A name that matches no plan, or several, is an answer rather than an error:
// the model is told their plans and asks, and nothing is deleted.
func deleteMealPlan(plans *meals.MealPlanService) Capability {
	type args struct {
		Plan string `json:"plan"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "delete_meal_plan",
			Description: "Delete one of this person's meal plans, with all its days and meals, when they ask to remove it. " +
				"Name the plan exactly as get_meal_plan or their plans list shows it. If no plan or several plans have that " +
				"name, nothing is deleted and their plans are listed so you can ask which they mean.",
			Parameters: ai.Object("which plan to delete", map[string]*ai.Schema{
				"plan": ai.String("the plan's name, as the person or get_meal_plan shows it"),
			}, "plan"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			list, err := plans.ListPlans(ctx, userID)
			if err != nil {
				return "", err
			}
			if len(list) == 0 {
				return "This person has no meal plans, so there is nothing to delete.", nil
			}

			hits := plansNamed(list, in.Plan)
			switch len(hits) {
			case 0:
				return fmt.Sprintf("Nothing was deleted: no meal plan is called %q. Their plans are %s; ask which one they mean.",
					strings.TrimSpace(in.Plan), mealPlanNames(list)), nil
			case 1:
				if err = plans.DeletePlan(ctx, hits[0].ID, userID); err != nil {
					return "", err
				}
				return fmt.Sprintf("Deleted %s.", hits[0].Name), nil
			default:
				return fmt.Sprintf("Nothing was deleted: %d meal plans are called %q (%s). Ask which one they mean; "+
					"if they cannot tell them apart, they can rename one in the app first.",
					len(hits), hits[0].Name, describePlanChoices(hits)), nil
			}
		},
	}
}

// plansNamed is every plan whose whole name is name, ignoring case and
// surrounding space.
func plansNamed(list []meals.MealPlan, name string) []meals.MealPlan {
	needle := strings.ToLower(strings.TrimSpace(name))
	var hits []meals.MealPlan
	if needle == "" {
		return hits
	}
	for _, p := range list {
		if strings.ToLower(strings.TrimSpace(p.Name)) == needle {
			hits = append(hits, p)
		}
	}
	return hits
}

// describePlanChoices tells same-named plans apart by what a person can see:
// how many days each has and when it last changed.
func describePlanChoices(list []meals.MealPlan) string {
	out := make([]string, len(list))
	for i, p := range list {
		out[i] = fmt.Sprintf("%s with %d days, last changed %s",
			strconv.Quote(p.Name), len(p.Days), p.UpdatedAt.Format("2 Jan 2006"))
	}
	return strings.Join(out, "; ")
}
