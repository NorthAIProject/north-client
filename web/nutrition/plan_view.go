package nutrition

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
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
func optionTabLabel(o meal.Meal, i int) string {
	if o.OptionLabel != "" {
		return o.OptionLabel
	}
	return fmt.Sprintf("Option %d", i+1)
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

func daysLabel(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

func modeLabel(m meal.Mode) string {
	if m == meal.Advanced {
		return "Advanced"
	}
	return "Easy"
}

func planTypeLabel(s meal.PlanSettings) string {
	if s.Type == meal.Custom && s.CustomCarbPct != nil {
		return fmt.Sprintf("Custom · %.0f%% of carbs", *s.CustomCarbPct)
	}
	return s.Type.Label()
}

// bandLabel describes a preset plan type with its carb share and, once there
// is a target, the grams a day gets by default.
func bandLabel(b meal.CarbBand, target *meal.Macros) string {
	label := fmt.Sprintf("%s · %.0f–%.0f%% of your carbs", b.Label, b.MinPct, b.MaxPct)
	if target != nil {
		label += fmt.Sprintf(" · about %.0f g a day", b.CarbG(target.CarbG))
	}
	return label
}

func targetLine(t meal.Macros) string {
	return fmt.Sprintf("%.0f kcal · %.0f g protein · %.0f g carbs · %.0f g fat", t.Calories, t.ProteinG, t.CarbG, t.FatG)
}

// overrideLabel names a day's departure from the plan, empty when it has none.
func overrideLabel(o meal.DayOverride) string {
	var parts []string
	switch {
	case o.CarbType != nil:
		parts = append(parts, o.CarbType.Label()+" day")
	case o.CarbG != nil:
		parts = append(parts, fmt.Sprintf("%.0f g carbs", *o.CarbG))
	}
	if o.ProteinG != nil {
		parts = append(parts, fmt.Sprintf("%.0f g protein", *o.ProteinG))
	}
	if o.FatG != nil {
		parts = append(parts, fmt.Sprintf("%.0f g fat", *o.FatG))
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

func macroRows(s meal.DayStatus) []macroRow {
	return []macroRow{
		{Name: "Protein", Key: "protein", Consumed: s.Consumed.ProteinG, Target: s.Target.ProteinG, Over: s.Over.ProteinG},
		{Name: "Carbs", Key: "carbs", Consumed: s.Consumed.CarbG, Target: s.Target.CarbG, Over: s.Over.CarbG},
		{Name: "Fat", Key: "fat", Consumed: s.Consumed.FatG, Target: s.Target.FatG, Over: s.Over.FatG},
	}
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

func (m macroRow) summary() string {
	if m.isOver() {
		return fmt.Sprintf("%.0f / %.0f g · %.0f g over", m.Consumed, m.Target, m.Over)
	}
	return fmt.Sprintf("%.0f / %.0f g · %.0f g left", m.Consumed, m.Target, m.Target-m.Consumed)
}

// overageLine describes one day of an overage notice.
func overageLine(d meal.DayOverage) string {
	var parts []string
	for _, m := range macroRows(d.Status) {
		if m.isOver() {
			parts = append(parts, fmt.Sprintf("%s %.0f g over (%.0f of %.0f g)", strings.ToLower(m.Name), m.Over, m.Consumed, m.Target))
		}
	}
	return d.Weekday.String() + ": " + strings.Join(parts, ", ")
}

// previewState seeds the add-food form's live preview with what the day has
// left, so it can show what would remain after the chosen portion. The
// server still checks the portion when it is added.
func previewState(s meal.DayStatus) string {
	return fmt.Sprintf(`{ left: { protein: %.1f, carbs: %.1f, fat: %.1f }, id: "", grams: 0 }`,
		s.Remaining.ProteinG, s.Remaining.CarbG, s.Remaining.FatG)
}

// remainingLine is what a day has left before anything is picked.
func remainingLine(s meal.DayStatus) string {
	var parts []string
	for _, m := range macroRows(s) {
		if m.isOver() {
			parts = append(parts, fmt.Sprintf("%s %.0f g over", strings.ToLower(m.Name), m.Over))
		} else {
			parts = append(parts, fmt.Sprintf("%s %.0f g left", strings.ToLower(m.Name), m.Target-m.Consumed))
		}
	}
	return "This day: " + strings.Join(parts, " · ")
}

// previewText is the Alpine expression for what the day would have left after
// the chosen portion, reading the per-100g table the page embeds as
// #plan-ingredients.
const previewText = `(() => {
	const per100 = JSON.parse(document.getElementById('plan-ingredients').textContent)[id] || {};
	return 'After this: ' + ['protein', 'carbs', 'fat'].map(k => {
		const left = this.left[k] - (per100[k] || 0) * grams / 100;
		return k + ' ' + (left < -0.5 ? Math.round(-left) + ' g over' : Math.round(Math.max(left, 0)) + ' g left');
	}).join(' · ');
})()`

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
