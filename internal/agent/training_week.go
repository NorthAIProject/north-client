package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// Choosing what a week trains, and which plan to follow, from the chat.
//
// "I can only train three times this week" is the sentence these exist for.
// It changes one week, not the plan: each chosen day takes the next session
// of the plan's rotation, and whatever is left over carries into the week
// after. Plans and sessions are named the way people name them — a plan by
// its name, a session by its focus — never by id, for the reason
// workout_edits.go gives.
//
// set_training_week and set_active_workout_plan write, so each shows an
// approval card first (see coach.writingCalls).

func getTrainingWeek(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		NextWeek bool `json:"next_week"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "get_training_week",
			Description: "Read which days this person trains this week (or next week) and which session each day has, what is done, " +
				"and what is next. Use it before talking about their week, or before changing it with set_training_week.",
			Parameters: ai.Object("which week", map[string]*ai.Schema{
				"next_week": ai.Boolean("true for next week; this week otherwise"),
			}),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			var week workouts.WeekProgress
			if in.NextWeek {
				week, err = svc.NextWeek(ctx, user, time.Now())
			} else {
				week, err = svc.WeekProgress(ctx, user, time.Now())
			}
			if err != nil {
				return "", err
			}
			return describeWeek(week), nil
		},
	}
}

func setTrainingWeek(svc *workouts.Service, userSvc *users.Service) Capability {
	type sessionArg struct {
		Weekday string `json:"weekday"`
		Plan    string `json:"plan"`
		Session string `json:"session"`
	}
	type args struct {
		Days     *int         `json:"days"`
		Weekdays []string     `json:"weekdays"`
		Plan     string       `json:"plan"`
		Sessions []sessionArg `json:"sessions"`
		NextWeek bool         `json:"next_week"`
		Usual    bool         `json:"usual"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "set_training_week",
			Description: "Change which days this person trains this week or next, without changing their plan. " +
				"Give weekdays when they name them, or just days (a number) to have days suggested; days 0 is a rest week. " +
				"Each day gets the next session of their plan in order, and anything left over carries into the following week. " +
				"Sessions already trained this week are kept. Set plan to fill the days from another saved plan, and sessions to put a " +
				"specific session on a day. Set usual to put the week back to their usual one.",
			Parameters: ai.Object("the week to set", map[string]*ai.Schema{
				"days":     ai.Integer("how many days to train, 0 to 7, when they did not name the days"),
				"weekdays": ai.Array("the days to train, such as Monday", ai.String("a day of the week")),
				"plan":     ai.String("name of the saved plan whose sessions fill the days; the plan they follow when absent"),
				"sessions": ai.Array("days pinned to a specific session", ai.Object("one pinned day", map[string]*ai.Schema{
					"weekday": ai.String("the day, which must be one of the days trained"),
					"plan":    ai.String("name of the saved plan the session is from; the plan they follow when absent"),
					"session": ai.String("the session's focus as the plan writes it, such as 'Upper A'"),
				}, "weekday", "session")),
				"next_week": ai.Boolean("true to change next week instead of this one"),
				"usual":     ai.Boolean("true to put the week back to their usual one; other fields are ignored"),
			}),
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
			if in.Usual {
				week, err := svc.ResetWeek(ctx, user, time.Now(), in.NextWeek)
				if err != nil {
					return "", err
				}
				return "Put the week back to the usual one.\n" + describeWeek(week), nil
			}
			if in.Days == nil && len(in.Weekdays) == 0 {
				return "", fmt.Errorf("say how many days (days) or which days (weekdays) to train")
			}

			plans, err := svc.ListCurrentPlans(ctx, userID, 50)
			if err != nil {
				return "", err
			}
			change := workouts.WeekChange{Weekdays: in.Weekdays, NextWeek: in.NextWeek}
			if in.Days != nil {
				change.Days = *in.Days
			}
			if in.Plan != "" {
				p, err := pickPlan(plans, in.Plan)
				if err != nil {
					return "", err
				}
				change.PlanID = &p.ID
			}
			for _, s := range in.Sessions {
				ref, err := pickSession(plans, s.Plan, s.Session)
				if err != nil {
					return "", err
				}
				if change.Assign == nil {
					change.Assign = make(map[string]workouts.SessionRef)
				}
				change.Assign[s.Weekday] = ref
			}
			if len(change.Assign) > 0 && len(change.Weekdays) == 0 {
				return "", fmt.Errorf("name the weekdays when pinning sessions to them")
			}

			week, err := svc.SetWeek(ctx, user, time.Now(), change)
			if err != nil {
				return "", err
			}
			return "Saved the week.\n" + describeWeek(week), nil
		},
	}
}

func setActiveWorkoutPlan(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		Plan string `json:"plan"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "set_active_workout_plan",
			Description: "Switch which of this person's saved training plans they follow, by name. Nothing is generated or deleted; " +
				"the week switches to the chosen plan's usual days, keeping what was already trained. " +
				"For a different week only, use set_training_week with plan instead.",
			Parameters: ai.Object("the plan to follow", map[string]*ai.Schema{
				"plan": ai.String("the saved plan's name, or enough of it to be unambiguous"),
			}, "plan"),
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
			plans, err := svc.ListCurrentPlans(ctx, userID, 50)
			if err != nil {
				return "", err
			}
			chosen, err := pickPlan(plans, in.Plan)
			if err != nil {
				return "", err
			}
			active, err := svc.SetActivePlan(ctx, user, chosen.ID)
			if err != nil {
				return "", err
			}
			return "Now following:\n" + active.Plan.Summary(), nil
		},
	}
}

// pickPlan resolves a saved plan by the name a model used, the way pickGoal
// resolves a goal: ambiguity is an error listing the choices.
func pickPlan(plans []workouts.StoredPlan, name string) (workouts.StoredPlan, error) {
	needle := strings.TrimSpace(strings.ToLower(name))
	var hits []workouts.StoredPlan
	for _, p := range plans {
		lower := strings.ToLower(p.Plan.Name)
		if lower == needle {
			return p, nil
		}
		if needle != "" && strings.Contains(lower, needle) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return workouts.StoredPlan{}, fmt.Errorf("no saved plan matches %q (plans: %s)", name, planNames(plans))
	default:
		return workouts.StoredPlan{}, fmt.Errorf("%q matches several plans (%s); be more specific", name, planNames(hits))
	}
}

// pickSession resolves a session by its focus within a named plan, or within
// the plan followed — the first of plans — when no plan is named.
func pickSession(plans []workouts.StoredPlan, planName, focus string) (workouts.SessionRef, error) {
	if len(plans) == 0 {
		return workouts.SessionRef{}, fmt.Errorf("there are no saved plans")
	}
	p := plans[0]
	if planName != "" {
		var err error
		if p, err = pickPlan(plans, planName); err != nil {
			return workouts.SessionRef{}, err
		}
	}
	needle := strings.TrimSpace(strings.ToLower(focus))
	var hits []int
	var names []string
	for i, d := range p.Plan.Days {
		names = append(names, d.Focus)
		if strings.ToLower(d.Focus) == needle || strings.ToLower(d.Weekday) == needle {
			hits = append(hits, i)
		}
	}
	if len(hits) != 1 {
		return workouts.SessionRef{}, fmt.Errorf("%s has no single session called %q (sessions: %s)", p.Plan.Name, focus, strings.Join(names, ", "))
	}
	return workouts.SessionRef{PlanID: p.ID, DayIndex: hits[0]}, nil
}

func planNames(plans []workouts.StoredPlan) string {
	names := make([]string, 0, len(plans))
	for _, p := range plans {
		names = append(names, p.Plan.Name)
	}
	return strings.Join(names, ", ")
}

// describeWeek is a week as a short list the model can repeat back.
func describeWeek(week workouts.WeekProgress) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Week of %s", week.Start.Format("Mon 2 Jan"))
	if week.Custom {
		b.WriteString(" (changed from the usual week)")
	}
	b.WriteString(":\n")
	if len(week.Days) == 0 {
		b.WriteString("- a rest week, nothing scheduled\n")
	}
	for _, d := range week.Days {
		status := ""
		switch {
		case d.Completed:
			status = " — done"
		case week.HasNext && week.NextDay.Date.Equal(d.Date):
			status = " — next"
		}
		fmt.Fprintf(&b, "- %s: %s (%s)%s\n", d.Weekday, d.Day.Focus, d.PlanName, status)
	}
	return b.String()
}
