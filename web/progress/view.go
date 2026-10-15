// Package progress renders the progress-check page: the reminder cadence, a
// new check with photos and measurements, and the checks so far.
package progress

// Data is everything the page shows. Built by internal/progresschecks.
type Data struct {
	// Today is the person's local date, YYYY-MM-DD, the date field's default.
	Today string

	Cadence   string // weekly, biweekly, monthly; empty when no reminder is set
	NextDueOn string // a display date, empty with no reminder
	Due       bool

	// Last is the newest check, shown beside each pose for comparison.
	Last   *Check
	Checks []Check

	Assist Assist
	Form   Form
}

// Assist is where photos would go for an estimate. Zero when none may, and
// the page then offers the manual form only.
type Assist struct {
	Available bool
	// Label is the provider, named on the page before anything is sent.
	Label string
	// Own is the person's own connected key.
	Own bool
}

// EstimateNote is what an estimate just did, shown under the numbers.
type EstimateNote struct {
	Confidence string // low, medium, high
	Notes      string
	// Filled is how many empty fields it filled; numbers already typed stay.
	Filled int
	// Kept is how many it estimated but left alone because a number was typed.
	Kept int
}

// Check is one saved check as the history shows it.
type Check struct {
	ID       string
	Date     string
	Chips    []Chip
	Photos   []Photo
	Assisted bool
	Complete bool
}

// Photo returns the check's photo for a pose, if it has one.
func (c Check) Photo(pose string) (Photo, bool) {
	for _, p := range c.Photos {
		if p.Pose == pose {
			return p, true
		}
	}
	return Photo{}, false
}

// Chip is one measurement: a catalogue key for its label, the value with its
// unit, and how it moved since the check before, if that had it too.
type Chip struct {
	Key   string
	Value string
	Delta string
	// Carried is a weight copied from the last known one, not a reading.
	Carried bool
}

// Photo is one pose; URL is the app's /media route, which signs it.
type Photo struct {
	Pose string
	URL  string
}

// Form is the new-check form's values, kept when a save is refused.
type Form struct {
	MeasuredOn string
	Weight     string
	Waist      string
	Hips       string
	Chest      string
	Thigh      string
	Arm        string
	Errors     map[string]string

	// Source is "assisted" once an estimate filled a field; the save carries
	// it with the estimate's confidence and notes.
	Source           string
	AssistConfidence string
	AssistNotes      string

	// Estimate is the estimate just shown; EstimateError why one failed.
	Estimate      *EstimateNote
	EstimateError string
}

func (f Form) fieldError(name string) string { return f.Errors[name] }

// Poses are the slots, in order; Required marks the two a complete check needs.
var Poses = []struct {
	Name     string
	Required bool
}{
	{"front", true},
	{"side", true},
	{"back", false},
}

// measurement is one number field of the form.
type measurement struct {
	Name  string // form field
	Field string // the service's field-error key
	Key   string // catalogue key for the label
	Unit  string
	Value string
}

func (f Form) measurements() []measurement {
	return []measurement{
		{"weight", "weight_kg", "progress.m.weight", "kg", f.Weight},
		{"waist", "waist_cm", "progress.m.waist", "cm", f.Waist},
		{"hips", "hips_cm", "progress.m.hips", "cm", f.Hips},
		{"chest", "chest_cm", "progress.m.chest", "cm", f.Chest},
		{"thigh", "thigh_cm", "progress.m.thigh", "cm", f.Thigh},
		{"arm", "arm_cm", "progress.m.arm", "cm", f.Arm},
	}
}
