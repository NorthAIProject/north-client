package agent

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/toolsurface"
	"github.com/NorthAIProject/north-client/internal/watches"
)

// createWatch sets up a standing task.
//
// Not ReadOnly, so the coach stops before running it and shows the standing-
// task card: what is watched, the schedule in words, the instruction, and
// Confirm / Not now. The row is written only after Confirm, and its result
// text is the sentence the coach then posts ("Got it — I'll watch X and ping
// you when Y") — see coach.ResolvePending.
func createWatch(svc *watches.Service) Capability {
	return Capability{
		Tool: ai.Tool{
			Name: watches.ToolName,
			Description: "Set up a standing task: something you check on a schedule and report on without being asked. " +
				"Use it when the person asks you to keep an eye on something, remind them regularly, or check in at a set time " +
				"(\"every morning tell me if my sleep dropped\", \"on Sundays look at my training week\"). " +
				"They confirm it on a card before it exists. Times are in their own timezone.",
			Parameters: ai.Object("the standing task", map[string]*ai.Schema{
				"watch":       ai.String("what to keep an eye on, as a short phrase that reads after \"I'll watch\": \"your sleep\""),
				"notify_when": ai.String("when to speak up, as a phrase that reads after \"ping you when\": \"it drops under seven hours\""),
				"spec":        ai.String("the full instruction you will follow each time it runs, in the second person"),
				"cadence":     ai.Enum("how often it runs", string(watches.CadenceDaily), string(watches.CadenceWeekly)),
				"weekday":     ai.String("for weekly: monday to sunday; for daily: an empty string"),
				"time":        ai.String("time of day in 24-hour HH:MM, like 08:00"),
			}, "watch", "notify_when", "spec", "cadence", "weekday", "time"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			p, err := watches.ParseProposal(raw)
			if err != nil {
				return "", err
			}
			if _, err = svc.Create(ctx, userID, toolsurface.Thread(ctx), p); err != nil {
				return "", err
			}
			return p.Confirmation(), nil
		},
	}
}
