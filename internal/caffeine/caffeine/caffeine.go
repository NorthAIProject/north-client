// Package caffeine holds the shape of a drink with caffeine in it, and the one
// piece of arithmetic that makes logging it worthwhile: how much is still
// active in the body now.
package caffeine

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// HalfLife is caffeine's elimination half-life in a typical adult. It varies
// from about three to seven hours between people; five is the figure most
// clinical references settle on, and the tile only claims to be approximate.
const HalfLife = 5 * time.Hour

// DailyLimitMG is the amount most health agencies call safe for a healthy
// adult in a day. The gauge fills against it.
const DailyLimitMG = 400

// Entry is one drink.
type Entry struct {
	ID       uuid.UUID
	LogDate  time.Time
	MG       int
	Label    string
	LoggedAt time.Time
}

// Preset is a common drink and a typical dose.
type Preset struct {
	Key  string
	Name string // English, for the feed; pages translate by Key
	MG   int
}

// Presets are the one-tap choices. Doses are typical servings, not promises:
// a coffee shop's "regular" can hold twice this.
func Presets() []Preset {
	return []Preset{
		{Key: "espresso", Name: "Espresso", MG: 63},
		{Key: "coffee", Name: "Coffee", MG: 100},
		{Key: "tea", Name: "Tea", MG: 45},
		{Key: "energy_drink", Name: "Energy drink", MG: 80},
		{Key: "cola", Name: "Cola", MG: 35},
	}
}

// PresetMG returns a preset's dose, or false for an unknown key.
func PresetMG(key string) (int, bool) {
	for _, p := range Presets() {
		if p.Key == key {
			return p.MG, true
		}
	}
	return 0, false
}

// Active is what is still circulating at now, by exponential decay from each
// drink. Drinks logged after now are ignored rather than counted early.
func Active(entries []Entry, now time.Time) float64 {
	total := 0.0
	for _, e := range entries {
		elapsed := now.Sub(e.LoggedAt)
		if elapsed < 0 {
			continue
		}
		total += float64(e.MG) * math.Pow(0.5, elapsed.Hours()/HalfLife.Hours())
	}
	return total
}

// Total adds a set of drinks up.
func Total(entries []Entry) int {
	total := 0
	for _, e := range entries {
		total += e.MG
	}
	return total
}

// Summary renders today's caffeine for the coach.
func Summary(entries []Entry, now time.Time) string {
	if len(entries) == 0 {
		return "Caffeine: none logged today."
	}
	last := entries[0].LoggedAt
	for _, e := range entries {
		if e.LoggedAt.After(last) {
			last = e.LoggedAt
		}
	}
	return fmt.Sprintf("Caffeine: %dmg today, about %.0fmg still active; last at %s.",
		Total(entries), Active(entries, now), last.Format("15:04"))
}

// DisplayName is how a drink reads in English: a preset's name for a preset
// key, the label as typed otherwise.
func DisplayName(label string) string {
	for _, p := range Presets() {
		if p.Key == label {
			return p.Name
		}
	}
	if label == "" {
		return "Caffeine"
	}
	return label
}

// PresetKeyFor is the preset a display name came from, if any.
func PresetKeyFor(name string) (string, bool) {
	for _, p := range Presets() {
		if p.Name == name {
			return p.Key, true
		}
	}
	return "", false
}
