package exercises

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
)

// Filters carries the raw query-string values back into the form, so a
// filtered page re-renders with its own controls still set. Raw rather than
// normalized: showing someone a filter they did not choose is more confusing
// than showing an unrecognised one that matched nothing.
type Filters struct {
	Query     string
	Muscle    string
	Category  string
	Equipment string
}

// Page is where the browse list sits in the catalog.
//
// The catalog reached 455 rows in one migration, so the list has to be paged.
// Offset paging with page links rather than a cursor: a page link has to jump
// to an arbitrary page, and it has to survive being bookmarked — the same
// reason the filter form below is a GET.
type Page struct {
	// Number is 1-based, and already clamped to Last by the handler, so the
	// template never has to reason about a page past the end.
	Number int
	Last   int

	// Total is every row matching the filter, not this page's worth.
	Total int

	// FirstRow is the 1-based index of this page's first row within the whole
	// result, for the "Showing 25-48 of 455" line.
	FirstRow int
}

// pageURL rebuilds the current filter at a different page.
//
// Every filter travels with the link. Dropping them would quietly reset the
// filter on page two, which reads as a page of results that do not match what
// was asked for.
//
// Page 1 omits the parameter, so the first page of a filter has one canonical
// URL rather than two that render identically.
func (f Filters) pageURL(page int) string {
	q := url.Values{}
	for key, value := range map[string]string{
		"q":         f.Query,
		"muscle":    f.Muscle,
		"category":  f.Category,
		"equipment": f.Equipment,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return "/app/exercises"
	}
	return "/app/exercises?" + q.Encode()
}

// anyIllustrated reports whether the credit line is owed on this page.
//
// The artwork covers 302 movements and the catalog carries 455, so a filtered
// view can legitimately show none of it — and crediting artwork that is not on
// the page is its own kind of wrong.
func anyIllustrated(found []exercise.Exercise) bool {
	for _, e := range found {
		if e.HasIllustration() {
			return true
		}
	}
	return false
}

// equipmentOptions are the values the catalog actually contains. Kept here
// rather than derived from plan.EquipmentNames(): the catalog carries "none"
// and "other", which are not equipment rules, and not every rule appears in
// the seed.
var equipmentOptions = []string{
	exercise.EquipmentNone,
	"barbell",
	"dumbbell",
	"kettlebell",
	"machine",
	"bench",
	"pull-up bar",
	"resistance band",
	"bike",
	exercise.EquipmentOther,
}

// humanize turns a stored value ("olympic_weightlifting", "pull-up bar") into
// something readable, without a second table to keep in sync.
func humanize(value string) string {
	if value == "" {
		return ""
	}
	spaced := strings.ReplaceAll(value, "_", " ")
	return strings.ToUpper(spaced[:1]) + spaced[1:]
}

// summary says which slice of the results is on screen.
//
// It used to say "Showing 60 of 186", which was true and useless: there was no
// way to reach the other 126. Now that page links exist it names the range, so
// the count on screen and the number of pages agree.
func summary(shown int, p Page) string {
	if shown == p.Total {
		return fmt.Sprintf("%d exercises", p.Total)
	}
	return fmt.Sprintf("Showing %d-%d of %d exercises", p.FirstRow, p.FirstRow+shown-1, p.Total)
}

// DetailView is the detail page: the exercise, and what the reader can do with
// it next.
type DetailView struct {
	Exercise exercise.Exercise

	// PlanID is the reader's current plan, uuid.Nil when they have none. Days
	// is that plan day by day, empty without one.
	PlanID uuid.UUID
	Days   []PlanDayOption

	// Similar is a few other movements for the same main muscle.
	Similar []exercise.Exercise
}

// PlanDayOption is one day of the plan, as the "add to" choice sees it.
type PlanDayOption struct {
	Index    int
	Weekday  string
	Focus    string
	Includes bool
}

func (v DetailView) HasPlan() bool { return v.PlanID != uuid.Nil }

// inPlanOn is the weekdays that already include the exercise, for the
// "already in your plan" line.
func (v DetailView) inPlanOn() []string {
	var out []string
	for _, day := range v.Days {
		if day.Includes {
			out = append(out, day.Weekday)
		}
	}
	return out
}

func (v DetailView) planHref() string {
	return "/app/training/" + v.PlanID.String()
}

// addHref is the plan edit that appends a catalog exercise to one day. Posted
// as a plain form, so the workouts handler answers it with a redirect to the
// plan rather than an htmx fragment.
func (v DetailView) addHref(day PlanDayOption) string {
	return fmt.Sprintf("/app/training/%s/days/%d/exercises", v.PlanID, day.Index)
}

// dayLabel names a plan day by weekday and focus, the way the plan page does.
func dayLabel(day PlanDayOption) string {
	if day.Focus == "" {
		return day.Weekday
	}
	return day.Weekday + " · " + day.Focus
}
