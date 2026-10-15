package stats

import (
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// kindKeys are the catalogue keys for the names kindName gives the activity
// families it folds together.
var kindKeys = map[string]string{
	"Running":        "stats.kind.running",
	"Cycling":        "stats.kind.cycling",
	"Walking":        "stats.kind.walking",
	"Swimming":       "stats.kind.swimming",
	"Indoor running": "stats.kind.indoor_running",
	"Indoor cycling": "stats.kind.indoor_cycling",
}

// KindLabel is an activity type's name, as kindName gives it, in tag's
// language — for display only.
//
// The name itself stays English everywhere else, because it is also the
// type's identifier: the cardio API returns it, and the kind page is found by
// it. A page that shows the type calls this; one that links to it keeps the
// name. A name from the MET table reads as that activity's catalogue entry;
// anything unknown reads as it is.
func KindLabel(tag, name string) string {
	if key, ok := kindKeys[name]; ok {
		return i18n.Translate(tag, key)
	}
	for _, m := range activity.METTable {
		if m.Name != name {
			continue
		}
		key := "activity.met." + m.Code
		if got := i18n.Translate(tag, key); got != key {
			return got
		}
		break
	}
	return name
}
