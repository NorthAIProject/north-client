package capture

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Parser turns a person's sentence into items nobody has resolved yet.
//
// An interface with one production implementation, for the reason
// memories.Extractor is one: it lets the service be tested against a struct
// literal instead of a model, and the alternative is a test that either calls a
// provider or asserts nothing.
type Parser interface {
	Parse(ctx context.Context, user users.User, text string, known []habits.Habit) (Draft, error)
}

// parseTemperature is zero: this is transcription, not writing. Two runs over
// the same sentence should not disagree about how much water it mentions.
var parseTemperature float32 = 0

// parseAttempts is how many times one provider may be asked before the walk
// moves on.
//
// Two, and no more. A malformed reply is not a provider refusing — ai.Failover
// answers false for a decode failure, so without this the walk stops dead and
// one bad reply costs the whole request. It matters unevenly: providers
// registered without JSON-schema support have the shape asked for in the prompt
// rather than enforced by the API (see internal/ai/openaicompat), so a reply
// that does not decode is an ordinary event there and nearly unreachable
// elsewhere.
//
// Not higher, because a capture is a cheap call with a person waiting on it,
// not a training plan.
const parseAttempts = 2

// modelResult is the shape the model answers in.
//
// Flat, with every field required and zero standing in for "not this kind".
// ai.Object makes every property required by construction, and its doc says
// why: a field the model may omit is a field the caller nil-checks forever.
// The typed Item this becomes is the shape the rest of the package uses.
type modelResult struct {
	Items    []modelItem `json:"items"`
	Unparsed []string    `json:"unparsed"`
}

type modelItem struct {
	Source     string  `json:"source"`
	Kind       string  `json:"kind"`
	Confidence string  `json:"confidence"`
	AmountML   int     `json:"amount_ml"`
	Minutes    int     `json:"minutes"`
	Quality    int     `json:"quality"`
	Date       string  `json:"date"`
	Habit      string  `json:"habit"`
	Weight     float64 `json:"weight"`
	WeightUnit string  `json:"weight_unit"`
	Mood       int     `json:"mood"`
	Energy     int     `json:"energy"`
	Notes      string  `json:"notes"`
	Food       string  `json:"food"`
	Grams      float64 `json:"grams"`
}

// Schema is the reply shape. Source comes first on purpose: PropertyOrdering
// is honoured by the provider, and a model that quotes the words before
// deciding what they mean decides better.
func Schema() *ai.Schema {
	item := ai.Object("one thing to log", map[string]*ai.Schema{
		"source":      ai.String("the exact words from their text this entry came from"),
		"kind":        ai.Enum("what this entry is", string(KindWater), string(KindSleep), string(KindHabit), string(KindWeight), string(KindCheckIn), string(KindFood)),
		"confidence":  ai.Enum("high when they stated the value outright, low when you converted or estimated it", "high", "low"),
		"amount_ml":   ai.Integer("water only: millilitres; 0 otherwise"),
		"minutes":     ai.Integer("sleep only: minutes slept; 0 otherwise"),
		"quality":     ai.Integer("sleep only: 1-5 if they said how well they slept, 0 if not"),
		"date":        ai.String("sleep only: the local date the night ended, YYYY-MM-DD; empty for last night"),
		"habit":       ai.String("habit only: the habit's name exactly as listed; empty otherwise"),
		"weight":      ai.Number("weight only: the number they said; 0 otherwise"),
		"weight_unit": ai.Enum("weight only: the unit they said it in", "kg", "lb", ""),
		"mood":        ai.Integer("check_in only: 1-5; 0 otherwise"),
		"energy":      ai.Integer("check_in only: 1-5; 0 otherwise"),
		"notes":       ai.String("check_in only: anything else they said about the day; empty otherwise"),
		"food":        ai.String("food only: what to look up, such as 'chicken breast'; empty otherwise"),
		"grams":       ai.Number("food only: how much they ate in grams; 0 otherwise"),
	},
		"source", "kind", "confidence", "amount_ml", "minutes", "quality", "date",
		"habit", "weight", "weight_unit", "mood", "energy", "notes", "food", "grams",
	)

	return ai.Object("what they logged", map[string]*ai.Schema{
		"items":    ai.Array("one entry per thing they logged; empty is better than a guess", item),
		"unparsed": ai.Array("every part of their text that became no entry, in their own words", ai.String("their words")),
	}, "items", "unparsed")
}

// RenderPrompt builds the system prompt for one capture.
//
// Exported so the evals in internal/capture/eval grade what production sends.
// internal/ai/eval's package doc gives the reason: a harness that hand-writes
// its own version of the prompt ends up grading a format the application no
// longer uses, and the drift is invisible until somebody trusts a passing
// suite.
func RenderPrompt(user users.User, text string, known []habits.Habit, now time.Time) (string, error) {
	names := make([]string, 0, len(known))
	for _, h := range known {
		names = append(names, h.Name)
	}

	system, err := prompts.Render(prompts.QuickCapture, map[string]any{
		"Now":      now.Format("Monday 2 January 2006, 15:04"),
		"Timezone": user.Timezone,
		"Habits":   names,
		"Text":     text,
	})
	if err != nil {
		return "", apperr.Wrap(err, "render the capture prompt")
	}
	return system, nil
}

// AIParser is the production Parser.
type AIParser struct {
	runner *ai.Runner
	model  string
}

// NewAIParser builds the parser. model may be empty for the chain's default.
func NewAIParser(runner *ai.Runner, model string) *AIParser {
	return &AIParser{runner: runner, model: model}
}

// Parse asks a model to transcribe one sentence into entries.
//
// It reads and never writes. That split is what makes the single confirmation
// on the other side honest: this call costs money and changes nothing, and
// Commit changes things and costs nothing.
func (p *AIParser) Parse(ctx context.Context, user users.User, text string, known []habits.Habit) (Draft, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Draft{}, apperr.Wrap(apperr.ErrValidation, "write down what you want to log first")
	}
	if len(text) > MaxText {
		return Draft{}, apperr.Wrap(apperr.ErrValidation, "that is longer than %d characters", MaxText)
	}

	now := time.Now().In(user.Location())
	system, err := RenderPrompt(user, text, known, now)
	if err != nil {
		return Draft{}, err
	}

	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfaceQuickCapture)

	var result modelResult
	_, err = p.runner.Run(ctx, ai.RunOptions{Tier: string(user.Tier)}, func(client ai.Client) error {
		// Each provider opens its own correction dialogue, for the reason
		// workouts does: carrying another model's malformed reply across would
		// ask this one to repair words it never wrote.
		messages := []ai.Message{ai.UserText(text)}

		for attempt := 1; attempt <= parseAttempts; attempt++ {
			resp, genErr := client.Generate(ctx, ai.Request{
				Model:          p.model,
				System:         system,
				Messages:       messages,
				ResponseSchema: Schema(),
				Temperature:    &parseTemperature,
			})
			if genErr != nil {
				return apperr.Wrap(genErr, "parse the capture")
			}

			var candidate modelResult
			decErr := json.Unmarshal([]byte(resp.Text), &candidate)
			if decErr == nil {
				result = candidate
				return nil
			}

			if attempt == parseAttempts {
				return apperr.Wrap(decErr, "the reply was not valid JSON for the required shape")
			}

			// Naming the failure is what makes the retry work; "try again"
			// produces the same reply. The model's own words go back with it,
			// because the correction only means anything against them.
			messages = append(messages,
				ai.ModelText(resp.Text),
				ai.UserText("That was not valid JSON matching the schema. Return the entries again, correctly."),
			)
		}
		return nil
	})
	if err != nil {
		return Draft{}, err
	}

	return build(result, text, now), nil
}

// build turns the model's reply into a draft, dropping what it got wrong and
// keeping what it missed.
func build(result modelResult, source string, now time.Time) Draft {
	var draft Draft

	for _, raw := range result.Items {
		if len(draft.Items) >= MaxItems {
			break
		}
		item, err := convert(raw, now)
		if err != nil {
			// A malformed entry becomes text the person can see rather than a
			// failed request. They said something; the parser mangled it; the
			// honest answer is to show the words back.
			if raw.Source != "" {
				draft.Unparsed = append(draft.Unparsed, raw.Source)
			}
			continue
		}
		draft.Items = append(draft.Items, item)
	}

	draft.Unparsed = append(draft.Unparsed, result.Unparsed...)
	draft.Unparsed = append(draft.Unparsed, leftovers(source, draft)...)
	draft.Unparsed = dedupe(draft.Unparsed)

	return draft
}

// convert maps one flat reply row onto a typed item.
func convert(raw modelItem, now time.Time) (Item, error) {
	item := Item{
		Kind:      Kind(strings.TrimSpace(raw.Kind)),
		Source:    raw.Source,
		Uncertain: strings.EqualFold(raw.Confidence, "low"),
	}

	switch item.Kind {
	case KindWater:
		item.Water = &Water{AmountML: raw.AmountML}
	case KindSleep:
		night := &Sleep{Minutes: raw.Minutes, Quality: raw.Quality}
		if raw.Date != "" {
			when, err := time.ParseInLocation("2006-01-02", raw.Date, now.Location())
			if err != nil {
				return Item{}, apperr.Wrap(apperr.ErrValidation, "unreadable date %q", raw.Date)
			}
			// A night in the future is a misread, not a plan.
			if when.After(now) {
				return Item{}, apperr.Wrap(apperr.ErrValidation, "date %q is in the future", raw.Date)
			}
			night.Date = when
		}
		item.Sleep = night
	case KindHabit:
		item.Habit = &Habit{Name: raw.Habit}
	case KindWeight:
		kg, err := toKilograms(raw.Weight, raw.WeightUnit)
		if err != nil {
			return Item{}, err
		}
		item.Weight = &Weight{KG: kg}
	case KindCheckIn:
		item.CheckIn = &CheckIn{Mood: raw.Mood, Energy: raw.Energy, Notes: raw.Notes}
	case KindFood:
		item.Food = &Food{Query: raw.Food, Grams: raw.Grams}
	default:
		return Item{}, apperr.Wrap(apperr.ErrValidation, "unknown kind %q", raw.Kind)
	}

	return Validate(item)
}

// toKilograms normalises a reading. The unit is the model's echo of the
// person's own words, so it is checked rather than assumed: silently reading
// "172 lb" as kilograms is a number nobody would question and everybody would
// be wrong about.
func toKilograms(value float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "kg", "":
		return value, nil
	case "lb", "lbs":
		return round1(value * 0.45359237), nil
	default:
		return 0, apperr.Wrap(apperr.ErrValidation, "unknown weight unit %q", unit)
	}
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

// leftovers finds the parts of the person's text that reached neither an item
// nor the model's own unparsed list.
//
// The prompt asks the model to report what it could not read, and a model that
// simply forgets a clause would otherwise delete it without anyone noticing.
// This makes "nothing is silently dropped" a property of the code rather than a
// hope about the prompt: whatever is left after removing every claimed span is
// split on the ordinary separators and shown back.
func leftovers(source string, draft Draft) []string {
	residue := strings.ToLower(source)

	claimed := make([]string, 0, len(draft.Items)+len(draft.Unparsed))
	for _, item := range draft.Items {
		claimed = append(claimed, item.Source)
	}
	claimed = append(claimed, draft.Unparsed...)

	// Longest first: removing "water" before "2L water" would leave "2L"
	// behind and report it as missed.
	for _, span := range byLengthDesc(claimed) {
		span = strings.ToLower(strings.TrimSpace(span))
		if span == "" {
			continue
		}
		residue = strings.ReplaceAll(residue, span, " ")
	}

	var out []string
	for _, fragment := range strings.FieldsFunc(residue, isSeparator) {
		fragment = strings.TrimSpace(fragment)
		if !meaningful(fragment) {
			continue
		}
		out = append(out, fragment)
	}
	return out
}

func isSeparator(r rune) bool {
	switch r {
	case ',', ';', '.', '\n', '\r', '!', '?':
		return true
	default:
		return false
	}
}

// meaningful filters the punctuation and stray connectives that removing spans
// leaves behind, so the preview does not accuse the person of saying "and".
func meaningful(fragment string) bool {
	if len([]rune(fragment)) < 3 {
		return false
	}

	letters := 0
	for _, r := range fragment {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			letters++
		}
	}
	if letters == 0 {
		return false
	}

	words := strings.Fields(fragment)
	for _, word := range words {
		if !isFiller(word) {
			return true
		}
	}
	return false
}

func isFiller(word string) bool {
	switch strings.Trim(strings.ToLower(word), "-—") {
	case "and", "then", "also", "plus", "with", "of", "the", "a", "an", "i", "my", "for", "at", "on", "in", "to", "today":
		return true
	default:
		return false
	}
}

func byLengthDesc(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && len(out[j]) > len(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if s == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}
