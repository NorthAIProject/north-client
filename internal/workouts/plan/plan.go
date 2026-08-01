// Package plan holds the shape of a training plan and the rules a generated
// one must satisfy.
//
// It is a leaf: the workouts service and the templates that render a plan both
// import it, and it imports neither. Keeping the types here is what lets a
// handler import its templates without the templates importing the handler's
// package back.
//
// This is North's differentiator, and the reason the AI layer supports
// schema-constrained output at all: a plan the application can store, validate,
// and render is worth something, and prose that mentions sets and reps is not.
package plan

import (
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// Plan is a training plan.
//
// The Go type and the schema handed to the model are defined in this one file,
// immediately next to each other. They describe the same thing, and separating
// them is how a field gets added to one and silently missed by the other.
type Plan struct {
	Name       string    `json:"name"`
	Rationale  string    `json:"rationale"`
	WeeksTotal int       `json:"weeks_total"`
	Days       []PlanDay `json:"days"`
}

type PlanDay struct {
	Weekday   string     `json:"weekday"`
	Focus     string     `json:"focus"`
	Exercises []Exercise `json:"exercises"`
}

type Exercise struct {
	Name string `json:"name"`
	Sets int    `json:"sets"`

	// Reps is a string because training is written that way: "8-12", "5",
	// "AMRAP". An integer would force every plan into a shape lifters do not
	// use.
	Reps string `json:"reps"`

	RestSeconds int    `json:"rest_seconds"`
	Equipment   string `json:"equipment"`
	FormCues    string `json:"form_cues"`

	// Substitute keeps a session possible when the gym is busy or a piece of
	// equipment is taken.
	Substitute string `json:"substitute"`
}

// PlanSchema is the shape the model must return.
//
// Property order is deliberate: the model writes the rationale before the days,
// so it commits to a structure in prose and then follows it. Asking for the
// conclusion first produces worse conclusions.
func PlanSchema() *ai.Schema {
	exercise := ai.Object("a single exercise", map[string]*ai.Schema{
		"name":         ai.String("the exercise, as a lifter would name it"),
		"sets":         ai.Integer("number of working sets"),
		"reps":         ai.String("rep range as written on a programme, such as 8-12, 5, or AMRAP"),
		"rest_seconds": ai.Integer("rest between sets, in seconds"),
		"equipment":    ai.String("equipment this needs; must be something the person said they have"),
		"form_cues":    ai.String("one short cue for the thing most likely to go wrong"),
		"substitute":   ai.String("an alternative if the equipment is unavailable"),
	}, "name", "sets", "reps", "rest_seconds", "equipment", "form_cues", "substitute")

	day := ai.Object("one training day", map[string]*ai.Schema{
		"weekday":   ai.String("which day, such as Monday"),
		"focus":     ai.String("what this session trains, such as lower body or push"),
		"exercises": ai.Array("exercises in the order they should be performed", exercise),
	}, "weekday", "focus", "exercises")

	return ai.Object("a training plan", map[string]*ai.Schema{
		"name":        ai.String("a short name for this plan"),
		"rationale":   ai.String("two or three sentences explaining why this plan suits this person, referencing their stated goal"),
		"weeks_total": ai.Integer("how many weeks to run this before reassessing"),
		"days":        ai.Array("training days, exactly as many as the person can train", day),
	}, "name", "rationale", "weeks_total", "days")
}

// Intake is what the person told us about their training.
type Intake struct {
	Goal           string
	Experience     string
	DaysPerWeek    int
	SessionMinutes int
	Equipment      []string
	Limitations    string
}

// Summary renders a plan compactly for the coach's context, so the chat coach
// can reference the plan the user is actually following instead of asking them
// to describe it again.
func (p Plan) Summary() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s (%d weeks)\n", p.Name, p.WeeksTotal)
	for _, d := range p.Days {
		fmt.Fprintf(&b, "  %s — %s: ", d.Weekday, d.Focus)
		for i, e := range d.Exercises {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s %dx%s", e.Name, e.Sets, e.Reps)
		}
		b.WriteString("\n")
	}

	return b.String()
}
