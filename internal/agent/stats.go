package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/users"
)

func clock(minutes int) string { return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60) }

// getStats reads the stats pages for the coach, so "how has my sleep been"
// and "does coffee affect my sleep" are answered from the numbers.
func getStats(svc *stats.Service, userSvc *users.Service) Capability {
	type args struct {
		Area  string `json:"area"`
		Range string `json:"range"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "get_stats",
			Description: "Read this person's stats for an area over a window: sleep (average, debt, bedtime consistency, stages), " +
				"cardio (distance, pace, best 5K, resting heart rate), eating (adherence to targets, protein per kg, top foods, late eating), " +
				"or patterns (what moves together across days, such as sleep after late caffeine). Use it before commenting on trends.",
			Parameters: ai.Object("what to read", map[string]*ai.Schema{
				"area":  ai.Enum("the area", "sleep", "cardio", "eating", "patterns"),
				"range": ai.Enum("the window; defaults to month", timerange.KeyWeek, timerange.KeyMonth, timerange.KeyQuarter, timerange.KeyYear),
			}, "area"),
		},
		ReadOnly:   true,
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			key := in.Range
			if key == "" {
				key = timerange.KeyMonth
			}
			rg := timerange.Parse(key, user.Location())
			var b strings.Builder
			switch in.Area {
			case "sleep":
				st, err := svc.Sleep(ctx, user, rg)
				if err != nil {
					return "", err
				}
				if len(st.Nights) == 0 {
					return "No sleep recorded in that window.", nil
				}
				fmt.Fprintf(&b, "%d nights, average %s; %d at 8h or more; debt over the last 7 nights %s.",
					len(st.Nights), clock(st.AvgMinutes), st.NightsOnTgt, clock(st.DebtMinutes))
				if st.HasTimes {
					fmt.Fprintf(&b, " Bedtime around %s (±%d min), up around %s (±%d min).", st.AvgBedtime, st.BedtimeSpread, st.AvgWake, st.WakeSpread)
				}
				if st.WeekdayAvg > 0 && st.WeekendAvg > 0 {
					fmt.Fprintf(&b, " Weekdays %s, weekends %s.", clock(st.WeekdayAvg), clock(st.WeekendAvg))
				}
				for stage, share := range st.StageShare {
					fmt.Fprintf(&b, " %s %.0f%%.", stage, share*100)
				}
			case "cardio":
				st, err := svc.Cardio(ctx, user, rg)
				if err != nil {
					return "", err
				}
				fmt.Fprintf(&b, "%d sessions, %s, %.1f km, %.0f kcal.", st.Sessions, clock(st.Seconds/60), st.DistanceKm, st.Kcal)
				for _, k := range st.ByKind {
					fmt.Fprintf(&b, " %s: %d sessions, %s.", k.Name, k.Sessions, clock(k.Seconds/60))
				}
				if r := st.Runs; r.Count > 0 && r.AvgPace > 0 {
					fmt.Fprintf(&b, " Runs: %d, average pace %d:%02d/km, best %d:%02d/km, longest %.1f km.",
						r.Count, int(r.AvgPace)/60, int(r.AvgPace)%60, int(r.BestPace)/60, int(r.BestPace)%60, r.LongestKm)
				}
				if n := len(st.RestingHR); n > 0 {
					fmt.Fprintf(&b, " Resting HR %.0f bpm (was %.0f).", st.RestingHR[n-1].Value, st.RestingHR[0].Value)
				}
			case "eating":
				st, err := svc.Eating(ctx, user, rg)
				if err != nil {
					return "", err
				}
				if st.DaysLogged == 0 {
					return "No food logged in that window.", nil
				}
				fmt.Fprintf(&b, "%d days logged, average %.0f kcal, P %.0f / C %.0f / F %.0f g.", st.DaysLogged, st.AvgKcal, st.AvgProtein, st.AvgCarb, st.AvgFat)
				if st.GoalKcal > 0 {
					fmt.Fprintf(&b, " Within 10%% of the %.0f kcal goal on %d days; protein goal met on %d.", st.GoalKcal, st.OnTargetDays, st.ProteinDays)
				}
				if st.ProteinPerKg > 0 {
					fmt.Fprintf(&b, " %.1f g protein per kg.", st.ProteinPerKg)
				}
				fmt.Fprintf(&b, " Ate late on %d days.", st.LateDays)
				for i, f := range st.TopFoods {
					if i == 5 {
						break
					}
					fmt.Fprintf(&b, " %s ×%d.", f.Label, f.Count)
				}
			case "patterns":
				found, days, err := svc.Patterns(ctx, user, rg)
				if err != nil {
					return "", err
				}
				if len(found) == 0 {
					return fmt.Sprintf("Nothing stands out over the last %d days yet.", days), nil
				}
				fmt.Fprintf(&b, "Over the last %d days (associations, not causes):", days)
				for _, f := range found {
					fmt.Fprintf(&b, "\n- %s. %s", f.Title, f.Detail)
				}
			default:
				return "", apperr.Wrap(apperr.ErrValidation, "area must be sleep, cardio, eating or patterns")
			}
			return b.String(), nil
		},
	}
}
