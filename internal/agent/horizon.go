package agent

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/horizon"
	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The forecast, from the chat: "where will my squat be by March?", "what if
// I take two weeks off?". Read-only, and arithmetic on their own history —
// the coach reports the ranges it is given and does not make up its own.

func getForecast(svc *horizon.Service, userSvc *users.Service) Capability {
	type args struct {
		SkipWeeks     int  `json:"skip_weeks"`
		ExtraSessions int  `json:"extra_sessions"`
		DeloadNow     bool `json:"deload_now"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "get_forecast",
			Description: "Read where this person's lifts, body weight and waist are heading over the next 12 weeks, as ranges worked out " +
				"from their own history, with how often such forecasts have held and whether a lighter week is worth suggesting. " +
				"Use it before answering any question about future progress, a plateau, or what a break would cost. " +
				"The optional arguments are a what-if. Always give the range, never a single number, and never say one thing " +
				"causes another: these are what tends to follow.",
			Parameters: ai.Object("an optional what-if", map[string]*ai.Schema{
				"skip_weeks":     ai.Integer("weeks from today with no training, 0 to 4"),
				"extra_sessions": ai.Integer("sessions a week added to their plan, 0 or 1; changes fatigue only, never the strength forecast"),
				"deload_now":     ai.Boolean("true to make their next training week a lighter one"),
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
			h, err := svc.Forecast(ctx, user, forecast.Scenario{SkipWeeks: in.SkipWeeks, ExtraSessions: in.ExtraSessions, DeloadNow: in.DeloadNow})
			if err != nil {
				return "", err
			}
			if brief := horizon.Brief(h, true); brief != "" {
				return brief, nil
			}
			return "Nothing can be forecast yet: there is too little history. It takes six sessions of a lift over four weeks, " +
				"five days of weigh-ins over two weeks, or three waist measurements over three weeks.", nil
		},
	}
}
