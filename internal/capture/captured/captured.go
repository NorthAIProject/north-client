// Package captured holds the shapes a capture turns into.
//
// A leaf package for the same reason habits/habit and workouts/plan are: the
// templates that render a draft and the service that writes one both need
// these types, and without a leaf they would have to import each other.
package captured

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Kind is what one captured item turns into.
type Kind string

const (
	KindWater   Kind = "water"
	KindSleep   Kind = "sleep"
	KindHabit   Kind = "habit"
	KindWeight  Kind = "weight"
	KindCheckIn Kind = "check_in"
	KindFood    Kind = "food"
)

// Kinds is every kind, in the order a preview lists them.
var Kinds = []Kind{KindWater, KindSleep, KindHabit, KindWeight, KindCheckIn, KindFood}

// Valid reports whether k is one of the known kinds.
func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// Bounds every parsed value has to sit inside.
//
// These are sanity rails, not medical opinion: they exist so a misread "2"
// cannot become a 2000 kg bodyweight, and they are wide enough that no real
// entry is refused.
const (
	MinWaterML = 1
	MaxWaterML = 5000

	MinSleepMinutes = 1
	MaxSleepMinutes = 24 * 60

	MinWeightKG = 20
	MaxWeightKG = 400

	MinFoodGrams = 1
	MaxFoodGrams = 5000

	MinScore = 1
	MaxScore = 5

	// MaxText bounds one capture. Long enough for a paragraph of a morning,
	// short enough that this never becomes a document upload — that is what
	// internal/documents is for.
	MaxText = 2000

	// MaxItems bounds one draft. A sentence that parsed into more than this
	// is a parse that went wrong, not a busy day.
	MaxItems = 25
)

// Item is one thing to log. Exactly one of the payload pointers is set, and
// which one is named by Kind.
//
// A tagged union rather than a flat struct with six unused fields: the JSON
// that crosses to the preview and back stays readable, and a payload that does
// not match its kind is a validation failure rather than a silently ignored
// field.
type Item struct {
	Kind Kind `json:"kind"`

	// Source is the span of the person's own text this came from. It is shown
	// beside the parsed value so the review answers "is that what I said?"
	// rather than "do I trust this?".
	Source string `json:"source"`

	// Uncertain marks a reading the parser was not confident about. It changes
	// nothing about how the item is written — it changes how loudly the
	// preview asks about it.
	Uncertain bool `json:"uncertain,omitempty"`

	// Problem is why this item cannot be written yet: a habit named that the
	// person does not keep, a food matching no ingredient. An item with a
	// Problem is shown and never committed, because the alternative is
	// inventing a row to hold it.
	Problem string `json:"problem,omitempty"`

	Water   *Water   `json:"water,omitempty"`
	Sleep   *Sleep   `json:"sleep,omitempty"`
	Habit   *Habit   `json:"habit,omitempty"`
	Weight  *Weight  `json:"weight,omitempty"`
	CheckIn *CheckIn `json:"check_in,omitempty"`
	Food    *Food    `json:"food,omitempty"`
}

// Water is a drink.
type Water struct {
	AmountML int `json:"amount_ml"`
}

// Sleep is one night, filed against the date it ended.
type Sleep struct {
	Minutes int `json:"minutes"`

	// Date is the local date the night is filed under. Zero means last night.
	Date time.Time `json:"date"`

	// Quality is 1..5, or 0 when the person did not say.
	Quality int `json:"quality,omitempty"`
}

// Habit is a completion of a habit the person already keeps.
//
// Capture never creates a habit. Starting to keep something is a decision, and
// a decision should not be a side effect of mentioning it once.
type Habit struct {
	Name string    `json:"name"`
	ID   uuid.UUID `json:"id,omitzero"`
}

// Weight is a bodyweight reading, always stored in kilograms.
type Weight struct {
	KG float64 `json:"kg"`
}

// CheckIn is today's mood and energy.
type CheckIn struct {
	Mood   int    `json:"mood"`
	Energy int    `json:"energy"`
	Notes  string `json:"notes,omitempty"`
}

// Food is one logged ingredient.
type Food struct {
	Query        string    `json:"query"`
	Grams        float64   `json:"grams"`
	IngredientID uuid.UUID `json:"ingredient_id,omitzero"`
	MatchedName  string    `json:"matched_name,omitempty"`
}

// Draft is a parse waiting for a person to agree with it.
type Draft struct {
	Items []Item `json:"items"`

	// Unparsed is the person's text that became nothing. Kept and shown rather
	// than dropped: silently swallowing half a sentence is how a log stops
	// being trusted.
	Unparsed []string `json:"unparsed,omitempty"`
}

// Writable reports whether one item can be committed as it stands.
//
// An item carrying a Problem, or naming a habit or food that never resolved to
// a row, is shown in the preview and never written. Inventing a row to hold it
// is the one thing this package must not do.
func Writable(item Item) bool {
	if item.Problem != "" {
		return false
	}
	switch item.Kind {
	case KindHabit:
		return item.Habit != nil && item.Habit.ID != uuid.Nil
	case KindFood:
		return item.Food != nil && item.Food.IngredientID != uuid.Nil
	default:
		return true
	}
}

// AnyWritable reports whether a draft has anything worth a Save button.
func (d Draft) AnyWritable() bool {
	for _, item := range d.Items {
		if Writable(item) {
			return true
		}
	}
	return false
}

// Outcome is what happened to one item.
type Outcome struct {
	Item Item `json:"item"`

	// Summary is the human sentence for a written item: "Logged 500 ml of
	// water." Empty when Error is set.
	Summary string `json:"summary,omitempty"`

	// Error is why this item was not written. Items are applied in order and
	// nothing rolls back, so a receipt may carry both kinds: a glass of water
	// that was logged is still true even if the habit tick after it failed.
	Error string `json:"error,omitempty"`
}

// Receipt is what a commit did.
type Receipt struct {
	Outcomes []Outcome `json:"outcomes"`
	Skipped  []Item    `json:"skipped,omitempty"`
}

// Written counts the items that reached a table.
func (r Receipt) Written() int {
	n := 0
	for _, o := range r.Outcomes {
		if o.Error == "" {
			n++
		}
	}
	return n
}

// Failed counts the items that did not.
func (r Receipt) Failed() int { return len(r.Outcomes) - r.Written() }

// Validate checks one item's shape and bounds and returns it cleaned.
//
// Called twice on purpose: once on what the model produced, and again on what
// the browser posted back. The second call is the one that matters — the
// preview round-trips through a hidden field, so nothing the client returns is
// trusted to still be what was sent.
//
// It deliberately says nothing about whether a habit or food name resolved to
// a real row. That is not a property of the value; it is a question about this
// account's data, and Writable answers it at the point the id is used.
func Validate(item Item) (Item, error) {
	if !item.Kind.Valid() {
		return Item{}, apperr.Wrap(apperr.ErrValidation, "unknown capture kind %q", item.Kind)
	}

	item.Source = strings.TrimSpace(item.Source)
	item.Problem = strings.TrimSpace(item.Problem)

	set := 0
	for _, present := range []bool{
		item.Water != nil, item.Sleep != nil, item.Habit != nil,
		item.Weight != nil, item.CheckIn != nil, item.Food != nil,
	} {
		if present {
			set++
		}
	}
	if set != 1 {
		return Item{}, apperr.Wrap(apperr.ErrValidation, "a %s item carries %d payloads, want exactly 1", item.Kind, set)
	}

	switch item.Kind {
	case KindWater:
		if item.Water == nil {
			return Item{}, payloadMismatch(item.Kind)
		}
		if err := between("water", item.Water.AmountML, MinWaterML, MaxWaterML); err != nil {
			return Item{}, err
		}
	case KindSleep:
		if item.Sleep == nil {
			return Item{}, payloadMismatch(item.Kind)
		}
		if err := between("sleep", item.Sleep.Minutes, MinSleepMinutes, MaxSleepMinutes); err != nil {
			return Item{}, err
		}
		if item.Sleep.Quality != 0 {
			if err := between("sleep quality", item.Sleep.Quality, MinScore, MaxScore); err != nil {
				return Item{}, err
			}
		}
	case KindHabit:
		if item.Habit == nil {
			return Item{}, payloadMismatch(item.Kind)
		}
		item.Habit.Name = strings.TrimSpace(item.Habit.Name)
		if item.Habit.Name == "" {
			return Item{}, apperr.Wrap(apperr.ErrValidation, "a habit item needs a name")
		}
	case KindWeight:
		if item.Weight == nil {
			return Item{}, payloadMismatch(item.Kind)
		}
		if item.Weight.KG < MinWeightKG || item.Weight.KG > MaxWeightKG {
			return Item{}, apperr.Wrap(apperr.ErrValidation,
				"weight %.1f kg is outside %d-%d", item.Weight.KG, MinWeightKG, MaxWeightKG)
		}
	case KindCheckIn:
		if item.CheckIn == nil {
			return Item{}, payloadMismatch(item.Kind)
		}
		if err := between("mood", item.CheckIn.Mood, MinScore, MaxScore); err != nil {
			return Item{}, err
		}
		if err := between("energy", item.CheckIn.Energy, MinScore, MaxScore); err != nil {
			return Item{}, err
		}
		item.CheckIn.Notes = strings.TrimSpace(item.CheckIn.Notes)
	case KindFood:
		if item.Food == nil {
			return Item{}, payloadMismatch(item.Kind)
		}
		item.Food.Query = strings.TrimSpace(item.Food.Query)
		if item.Food.Query == "" {
			return Item{}, apperr.Wrap(apperr.ErrValidation, "a food item needs something to look up")
		}
		if item.Food.Grams < MinFoodGrams || item.Food.Grams > MaxFoodGrams {
			return Item{}, apperr.Wrap(apperr.ErrValidation,
				"%.0f g is outside %d-%d", item.Food.Grams, MinFoodGrams, MaxFoodGrams)
		}
	}

	return item, nil
}

// ValidateAll cleans a whole draft's items, refusing the batch if any one of
// them is wrong. Partial acceptance would mean silently discarding something
// the person just reviewed and agreed to.
func ValidateAll(items []Item) ([]Item, error) {
	if len(items) == 0 {
		return nil, apperr.Wrap(apperr.ErrValidation, "there is nothing to log")
	}
	if len(items) > MaxItems {
		return nil, apperr.Wrap(apperr.ErrValidation, "that is more than %d entries at once", MaxItems)
	}

	out := make([]Item, 0, len(items))
	for i, item := range items {
		clean, err := Validate(item)
		if err != nil {
			return nil, apperr.Wrap(err, "entry %d", i+1)
		}
		out = append(out, clean)
	}
	return out, nil
}

func payloadMismatch(kind Kind) error {
	return apperr.Wrap(apperr.ErrValidation, "a %s item does not carry a %s payload", kind, kind)
}

func between(what string, got, low, high int) error {
	if got < low || got > high {
		return apperr.Wrap(apperr.ErrValidation, "%s %d is outside %d-%d", what, got, low, high)
	}
	return nil
}

// Summary is the sentence a written item gets in the receipt.
func Summary(item Item) string {
	switch item.Kind {
	case KindWater:
		return fmt.Sprintf("Logged %d ml of water.", item.Water.AmountML)
	case KindSleep:
		return fmt.Sprintf("Logged %s of sleep.", durationWords(item.Sleep.Minutes))
	case KindHabit:
		return fmt.Sprintf("Ticked off %s.", item.Habit.Name)
	case KindWeight:
		return fmt.Sprintf("Recorded %.1f kg.", item.Weight.KG)
	case KindCheckIn:
		return fmt.Sprintf("Logged today's check-in: mood %d, energy %d.", item.CheckIn.Mood, item.CheckIn.Energy)
	case KindFood:
		name := item.Food.MatchedName
		if name == "" {
			name = item.Food.Query
		}
		return fmt.Sprintf("Logged %.0f g of %s.", item.Food.Grams, name)
	default:
		return "Logged."
	}
}

func durationWords(minutes int) string {
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh %dm", h, m)
	}
}
