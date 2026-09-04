// Package eval grades the quick-capture parse.
//
// One set of cases (see Cases), graded at two depths, in the arrangement
// internal/ai/eval uses and for the same reason.
//
// Offline — parse_test.go, no build tag — asks whether the facts a case needs
// actually reach the model, by rendering the real capture.RenderPrompt. No
// provider, no database, no cost, so it runs on every push. It catches the
// regression that costs the most for the least effort: somebody edits the
// prompt and the habit list, the local date, or the "never invent an entry"
// rule quietly stops being sent.
//
// Live — parse_live_test.go, behind the `live` build tag — asks what a real
// model did with that prompt, by running the production AIParser against a real
// provider. These call a paid provider, so an ordinary `go test ./...` never
// spends money.
//
// The two tiers share their cases on purpose, and both grade the prompt the
// application actually sends rather than a copy.
//
// What is graded here that unit tests cannot reach: internal/capture's own
// tests hand modelItem values straight to convert, so they prove the
// conversions and the bounds. They say nothing about whether a model follows
// the instructions in the prompt — and those failures are the quiet ones. A
// mangled unit is refused loudly by Validate; an invented energy score to go
// with a stated mood is a plausible number in a log nobody re-reads.
package eval

import (
	"fmt"
	"math"
	"strings"

	"github.com/NorthAIProject/north-client/internal/capture/captured"
)

// Case is one capture scenario, graded at two depths.
//
// The same fixture answers two questions. Offline, the Prompt assertions ask
// whether what the model needs reached it. Live, the Draft assertions ask what
// it did with that. Keeping both on one Case is what stops the tiers drifting:
// there is one definition of "they keep two habits starting with morning".
type Case struct {
	// ID names the case in test output. Kebab-case, because it becomes the
	// subtest name and `go test -run` is easier to type without spaces.
	ID string

	// Why is one line on what breaking this case would cost the user. Printed
	// on failure, so whoever sees the red does not have to reconstruct the
	// intent from the assertions.
	Why string

	// Text is what the person typed.
	Text string

	// Habits are the habit names this person keeps. The parser is grounded in
	// them, so a case about naming a habit needs them and a case about
	// refusing to invent one needs them absent.
	Habits []string

	// Timezone is the person's, because "last night" is resolved against it.
	// Empty means Europe/Lisbon.
	Timezone string

	Prompt []PromptAssertion
	Draft  []DraftAssertion
}

// PromptAssertion grades the rendered system prompt. Deterministic: no
// provider, no network, no database.
type PromptAssertion interface {
	Name() string
	Check(system string, c Case) error
}

// DraftAssertion grades what the parser produced from a model's reply.
type DraftAssertion interface {
	Name() string
	Check(draft captured.Draft, c Case) error
}

// GradePrompt runs the prompt assertions, returning one message per failure.
//
// Returns strings rather than calling t.Errorf so this file stays free of
// testing: the same grading serves an offline test, a live test, and a report
// that counts a pass rate.
func (c Case) GradePrompt(system string) []string {
	var out []string
	for _, a := range c.Prompt {
		if err := a.Check(system, c); err != nil {
			out = append(out, fmt.Sprintf("assertion %s: %v", a.Name(), err))
		}
	}
	return out
}

// GradeDraft runs the draft assertions, returning one message per failure.
func (c Case) GradeDraft(draft captured.Draft) []string {
	var out []string
	for _, a := range c.Draft {
		if err := a.Check(draft, c); err != nil {
			out = append(out, fmt.Sprintf("assertion %s: %v", a.Name(), err))
		}
	}
	return out
}

// promptCheck adapts a function to PromptAssertion, so a new assertion is a
// constructor rather than a new type with two methods.
type promptCheck struct {
	name  string
	check func(system string, c Case) error
}

func (p promptCheck) Name() string { return p.name }

func (p promptCheck) Check(system string, c Case) error { return p.check(system, c) }

type draftCheck struct {
	name  string
	check func(draft captured.Draft, c Case) error
}

func (d draftCheck) Name() string { return d.name }

func (d draftCheck) Check(draft captured.Draft, c Case) error { return d.check(draft, c) }

// ---------------------------------------------------------------------------
// Prompt assertions
// ---------------------------------------------------------------------------

// Renders requires every want to appear in the rendered prompt, verbatim.
func Renders(want ...string) PromptAssertion {
	return promptCheck{
		name: "Renders",
		check: func(system string, _ Case) error {
			var missing []string
			for _, w := range want {
				if !strings.Contains(system, w) {
					missing = append(missing, fmt.Sprintf("%q", w))
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("the prompt is missing %s", strings.Join(missing, ", "))
			}
			return nil
		},
	}
}

// ListsTheirHabits requires every habit the person keeps to reach the prompt.
//
// The single highest-value piece of grounding available, and the one most
// likely to be lost in an edit: without it the model has no way to name a
// habit correctly and every habit entry becomes a guess.
func ListsTheirHabits() PromptAssertion {
	return promptCheck{
		name: "ListsTheirHabits",
		check: func(system string, c Case) error {
			for _, name := range c.Habits {
				if !strings.Contains(system, name) {
					return fmt.Errorf("habit %q never reached the prompt", name)
				}
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Draft assertions
// ---------------------------------------------------------------------------

// Water requires exactly one water entry, for the given millilitres.
func Water(ml int) DraftAssertion {
	return draftCheck{
		name: fmt.Sprintf("Water(%d)", ml),
		check: func(d captured.Draft, _ Case) error {
			item, err := only(d, captured.KindWater)
			if err != nil {
				return err
			}
			if item.Water.AmountML != ml {
				return fmt.Errorf("logged %d ml, want %d", item.Water.AmountML, ml)
			}
			return nil
		},
	}
}

// WaterBetween requires one water entry inside a range.
//
// For the inputs where no single number is correct — "a glass" is a judgement,
// and grading it exactly would be grading the model's taste rather than its
// reading.
func WaterBetween(low, high int) DraftAssertion {
	return draftCheck{
		name: fmt.Sprintf("WaterBetween(%d,%d)", low, high),
		check: func(d captured.Draft, _ Case) error {
			item, err := only(d, captured.KindWater)
			if err != nil {
				return err
			}
			if item.Water.AmountML < low || item.Water.AmountML > high {
				return fmt.Errorf("logged %d ml, want between %d and %d", item.Water.AmountML, low, high)
			}
			return nil
		},
	}
}

// Sleep requires one sleep entry of the given length, within a few minutes.
func Sleep(minutes int) DraftAssertion {
	return draftCheck{
		name: fmt.Sprintf("Sleep(%d)", minutes),
		check: func(d captured.Draft, _ Case) error {
			item, err := only(d, captured.KindSleep)
			if err != nil {
				return err
			}
			if abs(item.Sleep.Minutes-minutes) > 5 {
				return fmt.Errorf("logged %d minutes, want about %d", item.Sleep.Minutes, minutes)
			}
			return nil
		},
	}
}

// WeightKG requires one weight entry, in kilograms, within half a kilo.
//
// The tolerance is for the pound conversion, which is the point of the case:
// reading "172 lb" as 172 kg is a number nobody would question.
func WeightKG(kg float64) DraftAssertion {
	return draftCheck{
		name: fmt.Sprintf("WeightKG(%.1f)", kg),
		check: func(d captured.Draft, _ Case) error {
			item, err := only(d, captured.KindWeight)
			if err != nil {
				return err
			}
			if math.Abs(item.Weight.KG-kg) > 0.5 {
				return fmt.Errorf("logged %.1f kg, want about %.1f", item.Weight.KG, kg)
			}
			return nil
		},
	}
}

// Feels requires one check-in with both scores as stated.
func Feels(mood, energy int) DraftAssertion {
	return draftCheck{
		name: fmt.Sprintf("Feels(%d,%d)", mood, energy),
		check: func(d captured.Draft, _ Case) error {
			item, err := only(d, captured.KindCheckIn)
			if err != nil {
				return err
			}
			if item.CheckIn.Mood != mood || item.CheckIn.Energy != energy {
				return fmt.Errorf("logged mood %d energy %d, want %d and %d",
					item.CheckIn.Mood, item.CheckIn.Energy, mood, energy)
			}
			return nil
		},
	}
}

// HabitNamed requires one habit entry naming exactly this habit.
func HabitNamed(name string) DraftAssertion {
	return draftCheck{
		name: fmt.Sprintf("HabitNamed(%q)", name),
		check: func(d captured.Draft, _ Case) error {
			item, err := only(d, captured.KindHabit)
			if err != nil {
				return err
			}
			if !strings.EqualFold(strings.TrimSpace(item.Habit.Name), name) {
				return fmt.Errorf("named %q, want %q", item.Habit.Name, name)
			}
			return nil
		},
	}
}

// FoodAbout requires one food entry whose query mentions want, at roughly the
// stated weight.
func FoodAbout(want string, grams float64) DraftAssertion {
	return draftCheck{
		name: fmt.Sprintf("FoodAbout(%q,%.0f)", want, grams),
		check: func(d captured.Draft, _ Case) error {
			item, err := only(d, captured.KindFood)
			if err != nil {
				return err
			}
			if !strings.Contains(strings.ToLower(item.Food.Query), strings.ToLower(want)) {
				return fmt.Errorf("looked up %q, want something mentioning %q", item.Food.Query, want)
			}
			if math.Abs(item.Food.Grams-grams) > grams*0.5 {
				return fmt.Errorf("logged %.0f g, want roughly %.0f", item.Food.Grams, grams)
			}
			return nil
		},
	}
}

// NoneOfKind requires that nothing of these kinds was produced.
//
// The assertion behind most of this corpus. A parser that invents is worse
// than one that misses, because a missed line is visible in the leftovers and
// an invented one looks exactly like a real log.
func NoneOfKind(kinds ...captured.Kind) DraftAssertion {
	return draftCheck{
		name: "NoneOfKind",
		check: func(d captured.Draft, _ Case) error {
			for _, item := range d.Items {
				for _, kind := range kinds {
					if item.Kind == kind {
						return fmt.Errorf("produced a %s entry from %q", kind, item.Source)
					}
				}
			}
			return nil
		},
	}
}

// LogsNothing requires an empty draft. The right answer to a sentence that
// records nothing.
func LogsNothing() DraftAssertion {
	return draftCheck{
		name: "LogsNothing",
		check: func(d captured.Draft, _ Case) error {
			if len(d.Items) > 0 {
				return fmt.Errorf("logged %d entries from a sentence that records nothing: %s",
					len(d.Items), summarise(d))
			}
			return nil
		},
	}
}

// Counts requires exactly n entries.
func Counts(n int) DraftAssertion {
	return draftCheck{
		name: fmt.Sprintf("Counts(%d)", n),
		check: func(d captured.Draft, _ Case) error {
			if len(d.Items) != n {
				return fmt.Errorf("produced %d entries, want %d: %s", len(d.Items), n, summarise(d))
			}
			return nil
		},
	}
}

// UnparsedMentions requires each fragment to survive into the leftovers.
//
// Case-insensitive and substring, because the model echoes the person's words
// and small differences in what it quotes are not the failure being hunted.
// The failure being hunted is the clause vanishing entirely.
func UnparsedMentions(fragments ...string) DraftAssertion {
	return draftCheck{
		name: "UnparsedMentions",
		check: func(d captured.Draft, _ Case) error {
			joined := strings.ToLower(strings.Join(d.Unparsed, " | "))
			for _, fragment := range fragments {
				if !strings.Contains(joined, strings.ToLower(fragment)) {
					return fmt.Errorf("%q reached neither an entry nor the leftovers; leftovers were %v",
						fragment, d.Unparsed)
				}
			}
			return nil
		},
	}
}

// OnlyKnownHabits requires every habit entry to name a habit the person keeps.
//
// The coverage check in capture.build cannot catch this: an invented habit is
// well-formed, and it is the service that later refuses to resolve it. By then
// the person is reading a preview with a problem on it for a habit they never
// mentioned.
func OnlyKnownHabits() DraftAssertion {
	return draftCheck{
		name: "OnlyKnownHabits",
		check: func(d captured.Draft, c Case) error {
			for _, item := range d.Items {
				if item.Kind != captured.KindHabit {
					continue
				}
				var known bool
				for _, name := range c.Habits {
					if strings.EqualFold(strings.TrimSpace(item.Habit.Name), name) {
						known = true
					}
				}
				if !known {
					return fmt.Errorf("named %q, which they do not keep; they keep %v",
						item.Habit.Name, c.Habits)
				}
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------

// only returns the single entry of a kind, or says what it found instead.
func only(d captured.Draft, kind captured.Kind) (captured.Item, error) {
	var found []captured.Item
	for _, item := range d.Items {
		if item.Kind == kind {
			found = append(found, item)
		}
	}

	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return captured.Item{}, fmt.Errorf("no %s entry; got %s", kind, summarise(d))
	default:
		return captured.Item{}, fmt.Errorf("%d %s entries, want 1", len(found), kind)
	}
}

func summarise(d captured.Draft) string {
	if len(d.Items) == 0 {
		return "nothing"
	}
	parts := make([]string, 0, len(d.Items))
	for _, item := range d.Items {
		parts = append(parts, fmt.Sprintf("%s(%q)", item.Kind, item.Source))
	}
	return strings.Join(parts, ", ")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
