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
	"time"

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

	// CatalogSlug is the exercises-table slug the model picked, or empty when
	// it improvised past the catalog.
	//
	// The model echoes back an identifier it was shown rather than having its
	// free-text Name matched against the catalog afterwards: a name match has
	// to guess whether "Barbell Squat" is barbell-full-squat, and guessing
	// wrong attaches the wrong muscles to the movement.
	CatalogSlug string `json:"catalog_slug"`

	// IllustrationSlug names the pose artwork for this movement, copied from
	// the catalog row CatalogSlug resolved to. Empty when the model improvised
	// past the catalog, or when the catalog row has no artwork.
	//
	// Derived, never generated: PlanSchema deliberately does not ask the model
	// for it. A model naming an asset directory would be inventing a filename,
	// and a wrong one renders a picture of the wrong exercise.
	//
	// omitempty because plans are stored as JSONB and most predate this field;
	// see Service.PlanForDisplay, which fills it in on the way to the page
	// rather than leaving every existing plan without pictures.
	IllustrationSlug string `json:"illustration_slug,omitempty"`

	// Primary, Secondary, and Stabilizers are muscle keys from MuscleGroups
	// (NOR-8) — what the 3D viewer highlights for this exercise. Constrained
	// by PlanSchema's enum, so these never need fuzzy-matching against the
	// free-text exercise Name.
	//
	// When CatalogSlug resolves, these are overwritten from the catalog row
	// (see workouts.Service.Generate): a curated answer beats a generated one.
	// When it does not, the model's own keys stand — that is the free-text
	// fallback, and it is why these stay part of the schema.
	Primary     []string `json:"primary_muscles"`
	Secondary   []string `json:"secondary_muscles"`
	Stabilizers []string `json:"stabilizer_muscles"`
}

// PlanSchema is the shape the model must return.
//
// Property order is deliberate: the model writes the rationale before the days,
// so it commits to a structure in prose and then follows it. Asking for the
// conclusion first produces worse conclusions.
func PlanSchema() *ai.Schema {
	muscleGroup := func(desc string) *ai.Schema { return ai.Enum(desc, MuscleGroups...) }

	exercise := ai.Object("a single exercise", map[string]*ai.Schema{
		"name":               ai.String("the exercise, as a lifter would name it"),
		"catalog_slug":       ai.String("the slug of the catalog exercise you chose, copied exactly from the candidate list; empty string if you used an exercise that is not on that list"),
		"sets":               ai.Integer("number of working sets"),
		"reps":               ai.String("rep range as written on a programme, such as 8-12, 5, or AMRAP"),
		"rest_seconds":       ai.Integer("rest between sets, in seconds"),
		"equipment":          ai.String("equipment this needs; must be something the person said they have"),
		"form_cues":          ai.String("one short cue for the thing most likely to go wrong"),
		"substitute":         ai.String("an alternative if the equipment is unavailable"),
		"primary_muscles":    ai.Array("the one or two muscle groups doing most of the work", muscleGroup("a primary muscle group")),
		"secondary_muscles":  ai.Array("muscle groups meaningfully involved but not the main target", muscleGroup("a secondary muscle group")),
		"stabilizer_muscles": ai.Array("muscle groups bracing or stabilising the movement, without driving it", muscleGroup("a stabilizer muscle group")),
	}, "name", "catalog_slug", "sets", "reps", "rest_seconds", "equipment", "form_cues", "substitute", "primary_muscles", "secondary_muscles", "stabilizer_muscles")

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

// HasIllustration reports whether this exercise has pose artwork to render.
//
// Most plans have some exercises with and some without: the artwork covers 302
// movements, the catalog carries more, and the model may have improvised past
// the catalog entirely.
func (e Exercise) HasIllustration() bool {
	return e.IllustrationSlug != ""
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
		if emphasis := dayEmphasis(d); emphasis != "" {
			fmt.Fprintf(&b, " (emphasis: %s)", emphasis)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// NextSession is the training day that belongs to now, or the next one after
// it. Today wins when today is a plan day; otherwise the search walks forward
// through the week. Unrecognised weekday labels are ignored.
func (p Plan) NextSession(now time.Time) (PlanDay, bool) {
	byDay := make(map[time.Weekday]PlanDay, len(p.Days))
	for _, d := range p.Days {
		wd, ok := parseWeekday(d.Weekday)
		if !ok {
			continue
		}
		byDay[wd] = d
	}
	if len(byDay) == 0 {
		return PlanDay{}, false
	}

	for i := 0; i < 7; i++ {
		day := now.AddDate(0, 0, i)
		if session, ok := byDay[day.Weekday()]; ok {
			return session, true
		}
	}
	return PlanDay{}, false
}

func parseWeekday(label string) (time.Weekday, bool) {
	label = strings.TrimSpace(label)
	if label == "" {
		return 0, false
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), label) {
			return d, true
		}
	}
	return 0, false
}

// dayEmphasis lists the day's primary muscle groups once each, in the order
// they first appear across its exercises — the coach's read of "what does
// this session actually train", derived from the same muscle data the 3D
// viewer highlights (NOR-8), not a separate judgement.
func dayEmphasis(d PlanDay) string {
	seen := make(map[string]bool)
	var groups []string
	for _, e := range d.Exercises {
		for _, key := range e.Primary {
			if seen[key] {
				continue
			}
			seen[key] = true
			groups = append(groups, key)
		}
	}
	return strings.Join(groups, ", ")
}
