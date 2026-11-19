package horizon

import (
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
)

// Brief is the forecast as the coach reads it: English, compact, ranges
// only. Empty when nothing can be forecast yet, so the coach is not handed a
// line that says nothing.
//
// full adds every lift at four and twelve weeks, track records and what is
// missing; without it, it is the two or three lines worth carrying in every
// conversation.
func Brief(h Horizon, full bool) string {
	var lines []string
	for i, l := range h.Lifts {
		if l.Status != forecast.StatusReady {
			if full {
				lines = append(lines, fmt.Sprintf("%s: not enough recent sessions to forecast (%s).", l.Name, needText(l.Needs)))
			}
			continue
		}
		if !full && i > 1 {
			break
		}
		line := fmt.Sprintf("%s est. 1RM %.1f kg now, %s", l.Name, l.Now, l.Plateau)
		if r, ok := l.At(28); ok {
			line += fmt.Sprintf("; in 4 weeks %.1f–%.1f kg", r.Low, r.High)
		}
		if r, ok := l.At(84); ok && full {
			line += fmt.Sprintf("; in 12 weeks %.1f–%.1f kg", r.Low, r.High)
		}
		if full && l.Track.Shown() {
			line += fmt.Sprintf(" (4-week forecasts would have landed in range %d of %d times)", l.Track.Hits, l.Track.Total)
		}
		lines = append(lines, line+".")
	}
	if r, ok := h.Weight.At(28); ok {
		line := fmt.Sprintf("Body weight %.1f kg now; in 4 weeks %.1f–%.1f kg", h.Weight.Now, r.Low, r.High)
		if h.TargetWeightKg != nil {
			line += fmt.Sprintf("; target %.1f kg, %s", *h.TargetWeightKg, etaText(h.WeightETA))
		}
		lines = append(lines, line+".")
	} else if full {
		lines = append(lines, fmt.Sprintf("Body weight: cannot be forecast yet (%s).", needText(h.Weight.Needs)))
	}
	if r, ok := h.Waist.At(28); ok {
		lines = append(lines, fmt.Sprintf("Waist %.1f cm now; in 4 weeks %.1f–%.1f cm.", h.Waist.Now, r.Low, r.High))
	}
	if h.Deload.Suggested {
		lines = append(lines, "A lighter week is worth suggesting ("+strings.ReplaceAll(strings.Join(h.Deload.Reasons, ", "), "_", " ")+"). Suggest it; never apply it for them.")
	}
	if len(lines) == 0 {
		return ""
	}
	head := "Forecast from their own history (ranges, not promises; what tends to follow, not what causes what):"
	if !h.Scenario.IsZero() {
		head = fmt.Sprintf("What-if forecast (%d weeks off, %d extra session a week, lighter week now: %v). Time off only pauses progress, with no strength loss claimed; an extra session never raises a strength forecast:",
			h.Scenario.SkipWeeks, h.Scenario.ExtraSessions, h.Scenario.DeloadNow)
	}
	return head + "\n- " + strings.Join(lines, "\n- ")
}

func needText(needs []forecast.Need) string {
	parts := make([]string, 0, len(needs))
	for _, n := range needs {
		parts = append(parts, fmt.Sprintf("%s: %d of %d", strings.ReplaceAll(n.Code, "_", " "), n.Have, n.Want))
	}
	if len(parts) == 0 {
		return "too little history"
	}
	return strings.Join(parts, ", ")
}

func etaText(eta forecast.ETA) string {
	switch {
	case eta.Reached:
		return "reached"
	case !eta.Reachable:
		return "not on course for it in the next 12 weeks"
	case eta.Open:
		return fmt.Sprintf("in %d days at the earliest", eta.EarliestDays)
	default:
		return fmt.Sprintf("in %d to %d days", eta.EarliestDays, eta.LatestDays)
	}
}
