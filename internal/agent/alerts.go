package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/notifications"
)

func listAlerts(svc *notifications.Service) Capability {
	return Capability{
		Tool: ai.Tool{
			Name: "list_alerts",
			Description: "Show the user's configured coach alerts: photo check-in cadence and whether a reminder is set. " +
				"Use this before changing an alert, so you edit what they already have.",
			Parameters: ai.Object("no arguments", map[string]*ai.Schema{}),
		},
		ReadOnly:   true,
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, _ json.RawMessage) (string, error) {
			list, err := svc.ListSchedules(ctx, userID)
			if err != nil {
				return "", err
			}
			if len(list) == 0 {
				return "No alerts configured.", nil
			}
			var b strings.Builder
			for _, s := range list {
				fmt.Fprintln(&b, s.Line())
			}
			return strings.TrimSpace(b.String()), nil
		},
	}
}

func setAlert(svc *notifications.Service) Capability {
	type args struct {
		Kind         string `json:"kind"`
		Enabled      *bool  `json:"enabled"`
		EveryDays    int    `json:"every_days"`
		ReminderDays *int   `json:"reminder_days"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "set_alert",
			Description: "Create or change a recurring coach alert. " +
				"kind is currently only 'photo' (ask for a progress or form photo). " +
				"every_days is 7, 14, 21, or 28. " +
				"reminder_days is 0 (no reminder), 2, 3, or 7 — days after the ask if no photo arrived. " +
				"enabled turns the alert on or off without losing the cadence.",
			Parameters: ai.Object("the alert to set", map[string]*ai.Schema{
				"kind":          ai.Enum("which alert", notifications.KindPhoto),
				"enabled":       ai.Boolean("whether to send it"),
				"every_days":    ai.Integer("days between asks: 7, 14, 21, or 28"),
				"reminder_days": ai.Integer("days after the ask to remind if nothing arrived; 0 for none"),
			}, "kind"),
		},
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			current, err := svc.PhotoSchedule(ctx, userID)
			if err != nil {
				return "", err
			}
			if in.Kind != "" && in.Kind != notifications.KindPhoto {
				current.Kind = in.Kind
			}
			if in.Enabled != nil {
				current.Enabled = *in.Enabled
			}
			if in.EveryDays > 0 {
				current.EveryDays = in.EveryDays
			}
			if in.ReminderDays != nil {
				current.ReminderDays = *in.ReminderDays
			}

			saved, err := svc.UpsertSchedule(ctx, userID, notifications.ScheduleInput{
				Kind:         current.Kind,
				Enabled:      current.Enabled,
				EveryDays:    current.EveryDays,
				ReminderDays: current.ReminderDays,
			})
			if err != nil {
				return "", err
			}
			return "Updated. " + saved.Line(), nil
		},
	}
}
