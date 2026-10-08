package workoutsummary

import (
	"fmt"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// recentlyTrained is how far back a ready muscle still glows faintly, so the
// map shows the week's work and not only what is still tired.
const recentlyTrained = 7 * 24 * time.Hour

// Readiness is the muscle map as the card draws it: three tiers for the 3D
// viewer, and the muscles going stale beneath it.
type Readiness struct {
	Fatigued   []string
	Recovering []string
	// Fresh are muscles trained this past week that have already recovered.
	Fresh []string
	// Stale is each detrained muscle with how long since it was worked,
	// longest gap first: "Quads · 25 days".
	Stale []string
	When  string
}

// NewReadiness shapes a Load for the card.
func NewReadiness(l lift.Load) Readiness {
	r := Readiness{When: "Last session " + l.LastSession.In(l.At.Location()).Format("Mon 2 Jan")}
	for _, m := range l.Muscles {
		switch {
		case m.State == lift.StateFatigued:
			r.Fatigued = append(r.Fatigued, m.Muscle)
		case m.State == lift.StateRecovering:
			r.Recovering = append(r.Recovering, m.Muscle)
		case l.At.Sub(m.LastTrained) <= recentlyTrained:
			r.Fresh = append(r.Fresh, m.Muscle)
		}
	}
	for _, m := range l.Detrained() {
		days := int(l.At.Sub(m.LastTrained).Hours() / 24)
		r.Stale = append(r.Stale, fmt.Sprintf("%s · %d days", muscleLabel(m.Muscle), days))
	}
	return r
}

// muscleLabel turns a muscle key ("quads") into text ("Quads").
func muscleLabel(key string) string {
	if key == "" {
		return key
	}
	return strings.ToUpper(key[:1]) + key[1:]
}
