// Package supplement holds what a supplement is, the micronutrients North
// tracks coverage of, and which common supplements cover which.
package supplement

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Nutrients is the tracked set: the fifteen micronutrients most often short in
// an ordinary diet. The one list validation, the card's "0/15" and the UI's
// labels all read.
func Nutrients() []string {
	return []string{
		"vitamin_a", "vitamin_b12", "vitamin_c", "vitamin_d", "vitamin_e", "vitamin_k", "folate",
		"calcium", "iron", "magnesium", "zinc", "potassium", "iodine", "omega3", "fiber",
	}
}

// ValidNutrient reports whether n is in the tracked set.
func ValidNutrient(n string) bool {
	for _, known := range Nutrients() {
		if n == known {
			return true
		}
	}
	return false
}

// Preset is a common supplement and what it covers.
type Preset struct {
	Key       string
	Name      string
	Nutrients []string
}

// Presets are the one-tap choices.
func Presets() []Preset {
	return []Preset{
		{Key: "omega3", Name: "Omega-3 (EPA/DHA)", Nutrients: []string{"omega3"}},
		{Key: "vitamin_d3", Name: "Vitamin D3", Nutrients: []string{"vitamin_d"}},
		{Key: "multivitamin", Name: "Multivitamin", Nutrients: []string{
			"vitamin_a", "vitamin_b12", "vitamin_c", "vitamin_d", "vitamin_e", "vitamin_k", "folate", "zinc", "iodine",
		}},
		{Key: "magnesium", Name: "Magnesium", Nutrients: []string{"magnesium"}},
		{Key: "vitamin_c", Name: "Vitamin C", Nutrients: []string{"vitamin_c"}},
		{Key: "b12", Name: "Vitamin B12", Nutrients: []string{"vitamin_b12"}},
		{Key: "iron", Name: "Iron", Nutrients: []string{"iron"}},
		{Key: "zinc", Name: "Zinc", Nutrients: []string{"zinc"}},
		{Key: "calcium", Name: "Calcium", Nutrients: []string{"calcium"}},
		{Key: "psyllium", Name: "Psyllium husk", Nutrients: []string{"fiber"}},
		{Key: "creatine", Name: "Creatine", Nutrients: nil},
	}
}

// PresetFor finds a preset by key.
func PresetFor(key string) (Preset, bool) {
	for _, p := range Presets() {
		if p.Key == key {
			return p, true
		}
	}
	return Preset{}, false
}

// Entry is one supplement taken.
type Entry struct {
	ID        uuid.UUID
	LogDate   time.Time
	Name      string
	Count     int
	Nutrients []string
	LoggedAt  time.Time
}

// Label is "Omega-3 (EPA/DHA) ×3", or just the name for one.
func (e Entry) Label() string {
	if e.Count > 1 {
		return fmt.Sprintf("%s ×%d", e.Name, e.Count)
	}
	return e.Name
}

// Coverage is which tracked nutrients a day has, from supplements and any
// others the caller already knows are covered (from food or Apple Health).
type Coverage struct {
	Covered []string
	Missing []string
}

// CoverageFor works out the day's coverage, both lists in catalogue order.
func CoverageFor(entries []Entry, alsoCovered []string) Coverage {
	have := map[string]bool{}
	for _, e := range entries {
		for _, n := range e.Nutrients {
			have[n] = true
		}
	}
	for _, n := range alsoCovered {
		have[n] = true
	}
	var out Coverage
	for _, n := range Nutrients() {
		if have[n] {
			out.Covered = append(out.Covered, n)
		} else {
			out.Missing = append(out.Missing, n)
		}
	}
	return out
}

// Summary renders the day's supplements for the coach.
func Summary(entries []Entry) string {
	if len(entries) == 0 {
		return "Supplements: none logged today."
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Label()
	}
	sort.Strings(names)
	return "Supplements today: " + strings.Join(names, ", ") + "."
}
