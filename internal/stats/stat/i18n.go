package stat

import (
	"fmt"
	"math"
	"strings"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// The sentences Patterns and KindDetail write are English, and stay English:
// the coach reads them through the stats tool, and the coach's input is
// English whatever language it answers in. In rewords them for a person, at
// the point they are shown to one — never earlier, so the coach cannot be
// handed a translation by a context that happened to carry a locale.

// moodMeasures are the splits drawn at the person's own median of a measure.
// They share one sentence, and name the measure through a "what" phrase.
var moodMeasures = map[string]bool{"mood_steps": true, "mood_daylight": true, "mood_stand": true}

// In returns the finding worded in tag's language: title, detail and the two
// group labels. Numbers, keys and units are untouched. An unknown tag reads
// English; a finding the catalogue does not know comes back as it is.
//
// English goes through the catalogue too, rather than returning early, so a
// test can hold the catalogue's English to the sentences Patterns writes.
func (f Finding) In(tag string) Finding {
	prefix := "stats.pattern." + f.Key
	if moodMeasures[f.Key] {
		prefix = "stats.pattern.mood_measure"
	}
	if i18n.Translate(tag, prefix+".detail") == prefix+".detail" {
		return f
	}

	// thr is what the line was: a duration for screen time, a phrase naming
	// the measure for the mood splits, nothing for the fixed splits.
	var thr string
	switch {
	case f.Key == "sleep_screen":
		thr = mins(f.threshold)
	case moodMeasures[f.Key]:
		thr = i18n.TranslateF(tag, "stats.pattern."+f.Key+".what", measureValue(tag, f.Key, f.threshold))
	}

	direction := ".up"
	if f.Diff < 0 {
		direction = ".down"
	}

	out := f
	out.Title = i18n.TranslateF(tag, prefix+".title"+direction, amount(tag, f.Unit, f.Diff), thr)
	out.Detail = i18n.TranslateF(tag, prefix+".detail",
		amount(tag, f.Unit, f.A.Mean), f.A.N, amount(tag, f.Unit, f.B.Mean), f.B.N, thr)
	out.A.Label = i18n.Translate(tag, prefix+".a")
	if moodMeasures[f.Key] {
		out.A.Label = fmt.Sprintf(out.A.Label, thr)
	}
	out.B.Label = i18n.Translate(tag, prefix+".b")
	return out
}

// Localize is In over a list, for a caller about to show the list to someone.
func Localize(tag string, found []Finding) []Finding {
	out := make([]Finding, len(found))
	for i, f := range found {
		out[i] = f.In(tag)
	}
	return out
}

// amount writes a value in the finding's unit the way the English sentences
// do: minutes as "1h 05m", check-in points to one decimal, HRV and heart rate
// whole. Always the size, never the sign: the sentence says which way.
func amount(tag, unit string, v float64) string {
	switch unit {
	case "min":
		return mins(v)
	case "/5":
		return decimal(tag, math.Abs(v), 1)
	default:
		return decimal(tag, math.Abs(v), 0)
	}
}

// measureValue writes a mood split's line in the reader's notation.
func measureValue(tag, key string, v float64) string {
	switch key {
	case "mood_steps":
		return grouped(tag, v)
	case "mood_daylight":
		return mins(v)
	default:
		return decimal(tag, v, 0)
	}
}

// decimal is %.*f with the reader's decimal mark: a comma everywhere this
// build serves except English.
func decimal(tag string, v float64, places int) string {
	s := fmt.Sprintf("%.*f", places, v)
	if tag != i18n.DefaultLocale {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// grouped is a whole number with the reader's thousands separator: 11,000 in
// English, 11.000 in Spanish and Portuguese.
func grouped(tag string, v float64) string {
	s := thousands(v)
	if tag != i18n.DefaultLocale {
		s = strings.ReplaceAll(s, ",", ".")
	}
	return s
}

// In returns the kind with its bests' labels in tag's language. The name is
// left alone: it is the kind's identifier as much as its title, and the API
// hands it back to find the kind again.
func (k KindStats) In(tag string) KindStats {
	if len(k.Bests) == 0 {
		return k
	}
	out := k
	out.Bests = make([]Best, len(k.Bests))
	for i, b := range k.Bests {
		if b.labelKey != "" {
			b.Label = i18n.Translate(tag, b.labelKey)
		}
		out.Bests[i] = b
	}
	return out
}
