// Package sore holds where the body is sore, and the fixed set of regions the
// body view can point at.
package sore

import (
	"fmt"
	"strings"
	"time"
)

// Regions is every place soreness can be recorded, head to foot. The web's 3D
// body and the iOS one name their parts with these exact keys.
func Regions() []string {
	return []string{
		"neck", "shoulders", "chest", "upper_back", "lower_back", "abs", "biceps", "triceps",
		"forearms", "hips", "glutes", "quads", "hamstrings", "knees", "calves", "feet",
	}
}

// ValidRegion reports whether r is a known region.
func ValidRegion(r string) bool {
	for _, k := range Regions() {
		if k == r {
			return true
		}
	}
	return false
}

// Severity levels.
const (
	Stiff   = 1
	Sore    = 2
	Painful = 3
)

// Entry is one sore region on one day.
type Entry struct {
	LogDate  time.Time
	Region   string
	Severity int
	Note     string
	LoggedAt time.Time
}

// Summary renders a day's soreness for the coach.
func Summary(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	words := map[int]string{Stiff: "stiff", Sore: "sore", Painful: "painful"}
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("%s %s", strings.ReplaceAll(e.Region, "_", " "), words[e.Severity])
	}
	return "Soreness today: " + strings.Join(parts, ", ") + "."
}
