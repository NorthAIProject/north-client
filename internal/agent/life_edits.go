package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Changing goals, habits and the weight to aim at from the chat — the edits
// the goals, habits and settings pages offer, for people who would rather say
// "pause the Spanish goal" than find the button.

func updateGoal(svc *goals.Service) Capability {
	type args struct {
		GoalTitle  string `json:"goal_title"`
		Status     string `json:"status"`
		Title      string `json:"title"`
		TargetDate string `json:"target_date"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "update_goal",
			Description: "Change one of this person's goals: mark it achieved, pause, resume or abandon it, rename it, or set or clear its target date. " +
				"The goal is named by title. Give only what changes. For a progress note, use add_goal_update instead.",
			Parameters: ai.Object("the goal and what changes", map[string]*ai.Schema{
				"goal_title":  ai.String("the goal to change, matched by title"),
				"status":      ai.Enum("its new status; omit to keep it", goals.Statuses...),
				"title":       ai.String("a new title, in their words; omit to keep it"),
				"target_date": ai.String("a new target date as YYYY-MM-DD, or 'none' to make it open-ended; omit to keep it"),
			}, "goal_title"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			all, err := svc.List(ctx, userID)
			if err != nil {
				return "", err
			}
			goal, err := pickGoal(all, in.GoalTitle)
			if err != nil {
				return "", err
			}

			title, date := strings.TrimSpace(in.Title), strings.TrimSpace(in.TargetDate)
			if title != "" || date != "" {
				input := goals.Input{
					Title:      goal.Title,
					Motivation: goal.Motivation,
					Success:    goal.Success,
					Category:   goal.Category,
					TargetDate: goal.TargetDate,
				}
				if title != "" {
					input.Title = title
				}
				switch {
				case strings.EqualFold(date, "none"):
					input.TargetDate = time.Time{}
				case date != "":
					parsed, err := time.Parse(time.DateOnly, date)
					if err != nil {
						return "", fmt.Errorf("a target date is YYYY-MM-DD, not %q", date)
					}
					input.TargetDate = parsed
				}
				if goal, err = svc.Update(ctx, goal.ID, userID, input); err != nil {
					return "", err
				}
			}

			if in.Status != "" && in.Status != goal.Status {
				if goal, err = svc.SetStatus(ctx, goal.ID, userID, in.Status); err != nil {
					return "", err
				}
			}
			return "Updated the goal: " + goal.Summary(), nil
		},
	}
}

// habitDays turns weekday names into a habit's schedule. None means every
// day, which is what someone saying "I want to meditate" without naming days
// usually means.
func habitDays(names []string) ([]time.Weekday, error) {
	if len(names) == 0 {
		return []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}, nil
	}

	days := make([]time.Weekday, 0, len(names))
	for _, name := range names {
		needle := strings.ToLower(strings.TrimSpace(name))
		found := false
		for d := time.Sunday; d <= time.Saturday; d++ {
			if len(needle) >= 2 && strings.HasPrefix(strings.ToLower(d.String()), needle) {
				days = append(days, d)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%q is not a day of the week", name)
		}
	}
	return days, nil
}

func createHabit(svc *habits.Service, userSvc *users.Service) Capability {
	type args struct {
		Name   string   `json:"name"`
		Days   []string `json:"days"`
		Domain string   `json:"domain"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "create_habit",
			Description: "Start tracking a new habit for this person, when they have said they want to keep it — not for a passing wish. " +
				"Name it the way they said it. Leave days empty for every day.",
			Parameters: ai.Object("the habit", map[string]*ai.Schema{
				"name":   ai.String("the habit, such as 'read 20 pages'"),
				"days":   ai.Array("the days it is due, such as Monday; empty for every day", ai.String("a day of the week")),
				"domain": ai.Enum("the area of life it belongs to", lifedomain.Domains...),
			}, "name"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			days, err := habitDays(in.Days)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}

			habit, err := svc.Create(ctx, user, habits.Input{Name: in.Name, Domain: in.Domain, Days: days, Active: true})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Now tracking %q, due %s.", habit.Name, describeDays(habit.Days)), nil
		},
	}
}

func updateHabit(svc *habits.Service, userSvc *users.Service) Capability {
	type args struct {
		Name    string   `json:"name"`
		NewName string   `json:"new_name"`
		Days    []string `json:"days"`
		Active  *bool    `json:"active"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "update_habit",
			Description: "Change one of this person's habits: rename it, change the days it is due, or pause and resume it. " +
				"The habit is named as they call it. Give only what changes. To untick today, use undo_log with kind habit.",
			Parameters: ai.Object("the habit and what changes", map[string]*ai.Schema{
				"name":     ai.String("the habit to change, as they call it"),
				"new_name": ai.String("a new name; omit to keep it"),
				"days":     ai.Array("the new days it is due; omit to keep them", ai.String("a day of the week")),
				"active":   ai.Boolean("false to pause tracking it, true to resume; omit to keep it"),
			}, "name"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}

			// Paused habits too, so "resume the reading habit" can find one.
			list, err := svc.List(ctx, user, false)
			if err != nil {
				return "", err
			}
			habit, err := matchHabit(list, in.Name)
			if err != nil {
				return "", err
			}

			input := habits.Input{Name: habit.Name, Domain: habit.Domain, Days: habit.Days, Active: habit.Active}
			if name := strings.TrimSpace(in.NewName); name != "" {
				input.Name = name
			}
			if len(in.Days) > 0 {
				if input.Days, err = habitDays(in.Days); err != nil {
					return "", err
				}
			}
			if in.Active != nil {
				input.Active = *in.Active
			}

			updated, err := svc.Update(ctx, user, habit.ID, input)
			if err != nil {
				return "", err
			}
			state := "active"
			if !updated.Active {
				state = "paused"
			}
			return fmt.Sprintf("%q is %s, due %s.", updated.Name, state, describeDays(updated.Days)), nil
		},
	}
}

func describeDays(days []time.Weekday) string {
	if len(days) == 7 {
		return "every day"
	}
	names := make([]string, 0, len(days))
	for _, d := range days {
		names = append(names, d.String())
	}
	return strings.Join(names, ", ")
}

func setTargetWeight(svc *preferences.Service) Capability {
	type args struct {
		Kg float64 `json:"kg"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "set_target_weight",
			Description: "Set the body weight this person is aiming for, in kilograms, or clear it with 0. " +
				"Only a number they gave; never pick one for them.",
			Parameters: ai.Object("the target", map[string]*ai.Schema{
				"kg": ai.Number("the target weight in kg, or 0 to clear it"),
			}, "kg"),
		},
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			var kg *float64
			if in.Kg != 0 {
				kg = &in.Kg
			}
			if _, err := svc.SetTargetWeight(ctx, userID, kg); err != nil {
				return "", err
			}
			if kg == nil {
				return "Cleared the target weight.", nil
			}
			return fmt.Sprintf("Target weight set to %g kg.", *kg), nil
		},
	}
}
