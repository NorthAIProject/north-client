// Package award is the badge catalog: which badges exist, what area each
// belongs to and how much it takes to earn one. It imports nothing from the
// app so the achievements types, the web templates and the rule engine can all
// name a badge without importing one another.
//
// Names and descriptions are not here. They come from the i18n catalogues by
// badge id (awards.badge.<id>.name and .desc) at read time.
package award

// Area groups badges and decides whether they are private.
type Area string

const (
	Training    Area = "training"
	Lifts       Area = "lifts"
	Consistency Area = "consistency"
	Habits      Area = "habits"
	Nutrition   Area = "nutrition"
	Hydration   Area = "hydration"
	Sleep       Area = "sleep"
	Mind        Area = "mind"
	Goals       Area = "goals"
	Body        Area = "body"
	Social      Area = "social"
)

// Private is whether a badge in the area is for its owner's eyes alone: what
// someone eats, drinks, sleeps, thinks or weighs is not a thing to show off.
func (a Area) Private() bool {
	switch a {
	case Nutrition, Hydration, Sleep, Mind, Body:
		return true
	}
	return false
}

// Badge is one thing to earn. Target is the count the rule must reach.
type Badge struct {
	ID     string
	Area   Area
	Target int
	// Glyph is a lucide icon name.
	Glyph string
}

var catalog = []Badge{
	{"workouts_1", Training, 1, "footprints"},
	{"workouts_10", Training, 10, "dumbbell"},
	{"workouts_50", Training, 50, "flame"},
	{"workouts_100", Training, 100, "trophy"},
	{"early_bird", Training, 10, "sunrise"},
	{"listened_to_body", Training, 1, "heart-pulse"},
	{"pr_1", Lifts, 1, "medal"},
	{"pr_10", Lifts, 10, "award"},
	{"century", Lifts, 1, "weight"},
	{"streak_7", Consistency, 7, "calendar-check"},
	{"streak_30", Consistency, 30, "calendar-days"},
	{"streak_100", Consistency, 100, "zap"},
	{"streak_365", Consistency, 365, "crown"},
	{"habit_locked_in", Habits, 4, "lock"},
	{"habit_100", Habits, 100, "repeat"},
	{"meals_week", Nutrition, 7, "utensils"},
	{"protein_10", Nutrition, 10, "beef"},
	{"water_week", Hydration, 7, "droplet"},
	{"water_30", Hydration, 30, "waves"},
	{"rested_week", Sleep, 1, "moon"},
	{"nights_30", Sleep, 30, "bed"},
	{"journal_1", Mind, 1, "notebook-pen"},
	{"journal_10", Mind, 10, "book-open"},
	{"milestone_1", Goals, 1, "flag"},
	{"goal_on_time", Goals, 1, "target"},
	{"reviews_8", Goals, 8, "clipboard-check"},
	{"checks_4", Body, 4, "scan-line"},
	{"connected_30", Body, 30, "watch"},
	{"fasts_10", Body, 10, "timer"},
	{"first_follower", Social, 1, "user-plus"},
	{"crew_challenge", Social, 1, "users"},
}

// Catalog is every badge, in the order they are shown.
func Catalog() []Badge {
	return append([]Badge(nil), catalog...)
}

// ByID finds a badge. It reports false for an id that is not in the catalog,
// such as a row left by a badge that was later retired.
func ByID(id string) (Badge, bool) {
	for _, b := range catalog {
		if b.ID == id {
			return b, true
		}
	}
	return Badge{}, false
}
