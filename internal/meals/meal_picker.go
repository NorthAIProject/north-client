package meals

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
)

// mealOptionsFor is the meals of every plan that can be logged today, for
// the log-a-meal picker.
// MealPlanService has no "list all meals" query of its own, so this loads
// each plan in full — fine at the size a person's own meal plans
// realistically reach, not a per-message hot path.
func mealOptionsFor(ctx context.Context, plans *MealPlanService, userID uuid.UUID, today time.Weekday) ([]LoggableMeal, error) {
	list, err := plans.ListPlans(ctx, userID)
	if err != nil {
		return nil, err
	}

	full := make([]meal.MealPlan, 0, len(list))
	for _, p := range list {
		plan, err := plans.GetPlan(ctx, p.ID, userID)
		if err != nil {
			return nil, err
		}
		full = append(full, plan)
	}
	return mealPickerOptions(full, today), nil
}

// mealPickerOptions flattens each plan's meals, every option of every slot,
// into "Plan – Meal · Option" entries. A plan with a day for today offers
// only that day: an every-day plan would otherwise list each meal seven
// times over. A plan without one offers all its days, "Plan – Mon – Meal".
// An optional meal says so, "Plan – Ceia (optional)": logging it is a choice
// the plan leaves to the person.
func mealPickerOptions(plans []meal.MealPlan, today time.Weekday) []LoggableMeal {
	var opts []LoggableMeal
	for _, p := range plans {
		days, prefixDay := todaysDays(p, today)
		for _, d := range days {
			prefix := p.Name + " – "
			if prefixDay {
				prefix += d.Weekday.String()[:3] + " – "
			}
			for _, slot := range d.Meals {
				for _, m := range slot.Options() {
					label := prefix + m.DisplayName()
					if slot.Optional {
						label += " (optional)"
					}
					opts = append(opts, LoggableMeal{ID: m.ID.String(), Label: label})
				}
			}
		}
	}
	return opts
}

// todaysDays is the plan's day for today when it has one; otherwise every
// day, which then need their weekday to tell them apart.
func todaysDays(p meal.MealPlan, today time.Weekday) (days []meal.Day, prefixDay bool) {
	for _, d := range p.Days {
		if d.Weekday == today {
			return []meal.Day{d}, false
		}
	}
	return p.Days, true
}
