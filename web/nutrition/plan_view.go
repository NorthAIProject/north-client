package nutrition

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// PlanForm carries a submitted (or rejected) new meal plan.
type PlanForm struct {
	Name          string
	Description   string
	Mode          meal.Mode
	PlanType      meal.PlanType
	CustomCarbPct string
	DayCount      string
	Weekdays      []time.Weekday
	Errors        map[string]string
}

func (f PlanForm) fieldError(name string) string { return f.Errors[name] }

// mode is the form's mode, easy until the person picks.
func (f PlanForm) mode() meal.Mode {
	if f.Mode == "" {
		return meal.Easy
	}
	return f.Mode
}

// planType is the form's plan type, mid carb until the person picks.
func (f PlanForm) planType() meal.PlanType {
	if f.PlanType == "" {
		return meal.MidCarb
	}
	return f.PlanType
}

func (f PlanForm) dayCount() string {
	if f.DayCount == "" {
		return strconv.Itoa(meal.MaxDays)
	}
	return f.DayCount
}

func (f PlanForm) hasWeekday(wd time.Weekday) bool {
	for _, w := range f.Weekdays {
		if w == wd {
			return true
		}
	}
	return false
}

// PlanPage is a plan's detail page.
type PlanPage struct {
	Plan meal.MealPlan
	// Target is the person's current macro target; nil until the calculator
	// has produced one, and then nothing can be added.
	Target      *meal.Macros
	Ingredients []meal.Ingredient
	// Problem is a form on the page that was refused, shown next to it.
	Problem Problem
	// Overage is a change refused for taking a day over its target.
	Overage *OverageNotice
	// OpenOption is the ID of the meal option a change just landed on, whose
	// tab opens first; empty opens each slot's first option.
	OpenOption string
}

// Problem is a rejected form's field errors. Scope names the form, so two
// forms on the page with a "name" field do not both show the error.
type Problem struct {
	Scope  string
	Errors map[string]string
}

// OverageNotice explains an overage and, for an advanced plan, offers to
// re-send the same form with the overage confirmed.
type OverageNotice struct {
	Message    string
	CanConfirm bool
	Days       []meal.DayOverage
	Repost     Repost
}

// Repost is a submitted form, kept so it can be sent again unchanged.
type Repost struct {
	Action string
	Fields []RepostField
}

type RepostField struct {
	Name  string
	Value string
}

func (p PlanPage) errorFor(scope, field string) string {
	if p.Problem.Scope != scope {
		return ""
	}
	return p.Problem.Errors[field]
}

// statuses measures each day against the target, aligned with Plan.Days; nil
// without a target.
func (p PlanPage) statuses() []meal.DayStatus {
	if p.Target == nil {
		return nil
	}
	return p.Plan.State().Statuses(*p.Target)
}

// openOption is the option of a slot whose tab opens first: the one with a
// refused form, else the one a change just landed on, else the first.
func (p PlanPage) openOption(slot meal.Meal) string {
	open := slot.ID.String()
	for _, o := range slot.Options() {
		id := o.ID.String()
		if p.Problem.Scope == "portion:"+id {
			return id
		}
		if p.OpenOption == id {
			open = id
		}
	}
	return open
}

// optionTabLabel names an option in its slot's tabs: its label, or its
// position when it has none (an unlabelled default).
func optionTabLabel(ctx context.Context, o meal.Meal, i int) string {
	if o.OptionLabel != "" {
		return o.OptionLabel
	}
	return i18n.Tf(ctx, "nutrition.option.numbered", i+1)
}

func (p PlanPage) advanced() bool { return p.Plan.Settings.Mode == meal.Advanced }

// freeWeekdays are the weekdays the plan does not have yet, Monday first.
func (p PlanPage) freeWeekdays() []time.Weekday {
	var out []time.Weekday
	for _, wd := range meal.WeekOrder {
		taken := false
		for _, d := range p.Plan.Days {
			if d.Weekday == wd {
				taken = true
				break
			}
		}
		if !taken {
			out = append(out, wd)
		}
	}
	return out
}

func daysLabel(ctx context.Context, n int) string {
	if n == 1 {
		return i18n.T(ctx, "nutrition.days.one")
	}
	return i18n.Tf(ctx, "nutrition.days.many", n)
}

func modeLabel(ctx context.Context, m meal.Mode) string {
	if m == meal.Advanced {
		return i18n.T(ctx, "nutrition.mode.advanced")
	}
	return i18n.T(ctx, "nutrition.mode.easy")
}

// typeLabel is a plan type's name in the request's language. A type this page
// does not know reads as its stored value rather than as a catalogue key.
func typeLabel(ctx context.Context, t meal.PlanType) string {
	if _, ok := meal.BandFor(t); !ok && t != meal.Custom {
		return t.Label()
	}
	return i18n.T(ctx, "nutrition.type."+string(t))
}

// dayTypeLabel names a day that follows a preset carb level ("Low carb day").
func dayTypeLabel(ctx context.Context, t meal.PlanType) string {
	if _, ok := meal.BandFor(t); !ok {
		return t.Label()
	}
	return i18n.T(ctx, "nutrition.daytype."+string(t))
}

func planTypeLabel(ctx context.Context, s meal.PlanSettings) string {
	if s.Type == meal.Custom && s.CustomCarbPct != nil {
		return i18n.Tf(ctx, "nutrition.type.custom.pct", *s.CustomCarbPct)
	}
	return typeLabel(ctx, s.Type)
}

// bandLabel describes a preset plan type with its carb share and, once there
// is a target, the grams a day gets by default.
func bandLabel(ctx context.Context, b meal.CarbBand, target *meal.Macros) string {
	label := i18n.Tf(ctx, "nutrition.band", typeLabel(ctx, b.Type), b.MinPct, b.MaxPct)
	if target != nil {
		label += i18n.Tf(ctx, "nutrition.band.grams", b.CarbG(target.CarbG))
	}
	return label
}

func targetLine(ctx context.Context, t meal.Macros) string {
	return i18n.Tf(ctx, "nutrition.target.line", t.Calories, t.ProteinG, t.CarbG, t.FatG)
}

// weekdayName is a weekday as a heading or a button reads it, capitalised.
func weekdayName(ctx context.Context, wd time.Weekday) string {
	return capitalise(weekdayInSentence(ctx, wd))
}

// weekdayInSentence is a weekday as it reads mid-sentence: lower case in
// Spanish and Portuguese, capitalised in English.
func weekdayInSentence(ctx context.Context, wd time.Weekday) string {
	return i18n.T(ctx, "weekday.long."+strconv.Itoa(int(wd)))
}

func weekdayShort(ctx context.Context, wd time.Weekday) string {
	return i18n.T(ctx, "weekday.short."+strconv.Itoa(int(wd)))
}

func capitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// overrideLabel names a day's departure from the plan, empty when it has none.
func overrideLabel(ctx context.Context, o meal.DayOverride) string {
	var parts []string
	switch {
	case o.CarbType != nil:
		parts = append(parts, dayTypeLabel(ctx, *o.CarbType))
	case o.CarbG != nil:
		parts = append(parts, i18n.Tf(ctx, "nutrition.override.carbs", *o.CarbG))
	}
	if o.ProteinG != nil {
		parts = append(parts, i18n.Tf(ctx, "nutrition.override.protein", *o.ProteinG))
	}
	if o.FatG != nil {
		parts = append(parts, i18n.Tf(ctx, "nutrition.override.fat", *o.FatG))
	}
	return strings.Join(parts, " · ")
}

// macroRow is one macro of a day's status, for its progress bar.
type macroRow struct {
	Name     string
	Key      string
	Consumed float64
	Target   float64
	Over     float64
}

func macroRows(ctx context.Context, s meal.DayStatus) []macroRow {
	return []macroRow{
		{Name: i18n.T(ctx, "nutrition.macro.protein"), Key: "protein", Consumed: s.Consumed.ProteinG, Target: s.Target.ProteinG, Over: s.Over.ProteinG},
		{Name: i18n.T(ctx, "nutrition.macro.carbs"), Key: "carbs", Consumed: s.Consumed.CarbG, Target: s.Target.CarbG, Over: s.Over.CarbG},
		{Name: i18n.T(ctx, "nutrition.macro.fat"), Key: "fat", Consumed: s.Consumed.FatG, Target: s.Target.FatG, Over: s.Over.FatG},
	}
}

// inSentence is the macro's name as it reads inside a line ("protein 20 g
// left").
func (m macroRow) inSentence(ctx context.Context) string {
	return i18n.T(ctx, "nutrition.macro."+m.Key+".lower")
}

// percent is how much of the target is used, capped at 100 for the bar.
func (m macroRow) percent() int {
	if m.Target <= 0 {
		if m.Consumed > 0 {
			return 100
		}
		return 0
	}
	return int(min(100, m.Consumed/m.Target*100))
}

func (m macroRow) isOver() bool { return m.Over >= 0.5 }

func (m macroRow) summary(ctx context.Context) string {
	if m.isOver() {
		return i18n.Tf(ctx, "nutrition.macro.summary.over", m.Consumed, m.Target, m.Over)
	}
	return i18n.Tf(ctx, "nutrition.macro.summary.left", m.Consumed, m.Target, m.Target-m.Consumed)
}

// grams renders a whole number of grams for the strings that take it as text.
func grams(g float64) string { return strconv.FormatFloat(g, 'f', 0, 64) }

// overageLine describes one day of an overage notice.
func overageLine(ctx context.Context, d meal.DayOverage) string {
	var parts []string
	for _, m := range macroRows(ctx, d.Status) {
		if m.isOver() {
			parts = append(parts, i18n.Tf(ctx, "nutrition.overage.part", m.inSentence(ctx), m.Over, m.Consumed, m.Target))
		}
	}
	return weekdayName(ctx, d.Weekday) + ": " + strings.Join(parts, ", ")
}

// previewState seeds the add-food form's live preview with what the day has
// left, so it can show what would remain after the chosen portion. The
// server still checks the portion when it is added.
func previewState(s meal.DayStatus) string {
	return fmt.Sprintf(`{ left: { protein: %.1f, carbs: %.1f, fat: %.1f }, id: "", grams: 0 }`,
		s.Remaining.ProteinG, s.Remaining.CarbG, s.Remaining.FatG)
}

// remainingLine is what a day has left before anything is picked.
func remainingLine(ctx context.Context, s meal.DayStatus) string {
	var parts []string
	for _, m := range macroRows(ctx, s) {
		if m.isOver() {
			parts = append(parts, i18n.Tf(ctx, "nutrition.macro.over", m.inSentence(ctx), grams(m.Over)))
		} else {
			parts = append(parts, i18n.Tf(ctx, "nutrition.macro.left", m.inSentence(ctx), grams(m.Target-m.Consumed)))
		}
	}
	return i18n.Tf(ctx, "nutrition.remaining", strings.Join(parts, " · "))
}

// previewText is the Alpine expression for what the day would have left after
// the chosen portion, reading the per-100g table the page embeds as
// #plan-ingredients.
//
// The words come from the catalogue, formatted with {m} and {g} standing in
// for the macro and the grams, which the expression fills in as the person
// types. The same catalogue strings as remainingLine, so the line does not
// change wording when a food is picked.
func previewText(ctx context.Context) string {
	words, _ := json.Marshal(map[string]any{
		"after": i18n.Tf(ctx, "nutrition.preview.after", "{list}"),
		"over":  i18n.Tf(ctx, "nutrition.macro.over", "{m}", "{g}"),
		"left":  i18n.Tf(ctx, "nutrition.macro.left", "{m}", "{g}"),
		"names": map[string]string{
			"protein": i18n.T(ctx, "nutrition.macro.protein.lower"),
			"carbs":   i18n.T(ctx, "nutrition.macro.carbs.lower"),
			"fat":     i18n.T(ctx, "nutrition.macro.fat.lower"),
		},
	})
	return `(() => {
	const L = ` + string(words) + `;
	const per100 = JSON.parse(document.getElementById('plan-ingredients').textContent)[id] || {};
	return L.after.replace('{list}', ['protein', 'carbs', 'fat'].map(k => {
		const left = this.left[k] - (per100[k] || 0) * grams / 100;
		const line = left < -0.5 ? L.over.replace('{g}', Math.round(-left)) : L.left.replace('{g}', Math.round(Math.max(left, 0)));
		return line.replace('{m}', L.names[k]);
	}).join(' · '));
})()`
}

// ingredientMacros is each ingredient's per-100g macros by id, for the live
// preview.
func ingredientMacros(list []meal.Ingredient) map[string]map[string]float64 {
	out := make(map[string]map[string]float64, len(list))
	for _, in := range list {
		out[in.ID.String()] = map[string]float64{"protein": in.Per100g.ProteinG, "carbs": in.Per100g.CarbG, "fat": in.Per100g.FatG}
	}
	return out
}

func gramsValue(g *float64) string {
	if g == nil {
		return ""
	}
	return strconv.FormatFloat(*g, 'f', -1, 64)
}
