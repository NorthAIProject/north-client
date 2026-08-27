package exercises

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	"github.com/NorthAIProject/north-client/internal/users"
)

// These exist because the wiring they cover was once written and silently lost:
// the assets, the migration, the domain field and the serving path were all in
// place while this template still rendered none of them, and every test at the
// time passed. A render assertion is the only thing that catches that.

func renderBrowse(t *testing.T, found []exercise.Exercise, f Filters, p Page) string {
	t.Helper()
	var b strings.Builder
	if err := Browse(users.User{DisplayName: "Ada"}, found, f, p).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func renderDetail(t *testing.T, e exercise.Exercise) string {
	t.Helper()
	var b strings.Builder
	if err := Detail(users.User{DisplayName: "Ada"}, e).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func illustrated() exercise.Exercise {
	return exercise.Exercise{
		Slug:             "barbell-bench-press-medium-grip",
		Name:             "Barbell Bench Press - Medium Grip",
		Category:         exercise.CategoryStrength,
		Equipment:        "barbell",
		Difficulty:       exercise.DifficultyIntermediate,
		Primary:          []string{"chest"},
		Secondary:        []string{"triceps"},
		IllustrationSlug: "bench-press",
	}
}

func plainRow() exercise.Exercise {
	return exercise.Exercise{
		Slug:       "atlas-stones",
		Name:       "Atlas Stones",
		Category:   exercise.CategoryStrongman,
		Equipment:  exercise.EquipmentOther,
		Difficulty: exercise.DifficultyIntermediate,
		Primary:    []string{"erectors"},
	}
}

func onePage(total int) Page {
	return Page{Number: 1, Last: 1, Total: total, FirstRow: 1}
}

// The mask URL is the whole mechanism: the frames are painted through
// mask-image, and the path must ask for .svg even though the file on disk is
// .svg.gz — mountAssets bridges the two.
func TestBrowseRendersTheIllustrationForAnIllustratedExercise(t *testing.T) {
	t.Parallel()

	html := renderBrowse(t, []exercise.Exercise{illustrated()}, Filters{}, onePage(1))

	for _, want := range []string{
		"/assets/exercises/bench-press/frame-1.svg",
		"/assets/exercises/bench-press/frame-2.svg",
		"/assets/exercises/bench-press/frame-3.svg",
		"mask-image",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("browse row is missing %q", want)
		}
	}
	if strings.Contains(html, ".svg.gz") {
		t.Error("markup asks for .svg.gz; the storage format must not reach the template")
	}
}

func TestBrowseRendersNoIllustrationSlotForAnExerciseWithoutArtwork(t *testing.T) {
	t.Parallel()

	html := renderBrowse(t, []exercise.Exercise{plainRow()}, Filters{}, onePage(1))

	if strings.Contains(html, "/assets/exercises/") {
		t.Error("an exercise with no illustration_slug still rendered an illustration")
	}
	if !strings.Contains(html, "Atlas Stones") {
		t.Error("the row itself did not render")
	}
}

// CC BY-SA 4.0 requires visible credit wherever the artwork appears. Missing it
// is a licence breach, so it is asserted rather than assumed.
func TestBrowseCreditsTheArtworkOnlyWhenItIsShown(t *testing.T) {
	t.Parallel()

	withArt := renderBrowse(t, []exercise.Exercise{illustrated()}, Filters{}, onePage(1))
	if !strings.Contains(withArt, "creativecommons.org/licenses/by-sa/4.0") {
		t.Error("artwork rendered with no licence credit")
	}

	withoutArt := renderBrowse(t, []exercise.Exercise{plainRow()}, Filters{}, onePage(1))
	if strings.Contains(withoutArt, "creativecommons.org/licenses/by-sa/4.0") {
		t.Error("credited artwork that is not on the page")
	}
}

func TestDetailRendersTheIllustrationAndItsCredit(t *testing.T) {
	t.Parallel()

	html := renderDetail(t, illustrated())

	if !strings.Contains(html, "/assets/exercises/bench-press/frame-1.svg") {
		t.Error("detail page shows no illustration")
	}
	if !strings.Contains(html, "creativecommons.org/licenses/by-sa/4.0") {
		t.Error("detail page shows artwork with no licence credit")
	}
}

// Paging is the difference between 24 of 455 exercises and all of them. Before
// it existed the page rendered the first 60 and said so, with no way past.
func TestBrowseRendersPageLinksWhenThereIsMoreThanOnePage(t *testing.T) {
	t.Parallel()

	html := renderBrowse(t, []exercise.Exercise{plainRow()}, Filters{}, Page{
		Number: 2, Last: 19, Total: 455, FirstRow: 25,
	})

	if !strings.Contains(html, `href="/app/exercises?page=3"`) {
		t.Error("no link to the next page")
	}
	if !strings.Contains(html, `href="/app/exercises"`) {
		t.Error("no link back to page 1, which must be the bare URL")
	}
	// The window is 5 wide around page 2, so the last page needs its own link.
	if !strings.Contains(html, `href="/app/exercises?page=19"`) {
		t.Error("no link to the last page from a window that does not reach it")
	}
}

func TestBrowseRendersNoPageLinksForASinglePage(t *testing.T) {
	t.Parallel()

	html := renderBrowse(t, []exercise.Exercise{plainRow()}, Filters{}, onePage(1))
	if strings.Contains(html, `aria-label="pagination"`) {
		t.Error("rendered page links for a result that fits on one page")
	}
}

// A page link that drops the filter shows results that do not match what was
// asked for — the failure looks like a broken filter, not a broken link.
func TestPageLinksCarryEveryFilterForward(t *testing.T) {
	t.Parallel()

	f := Filters{Query: "bench press", Muscle: "chest", Category: "strength", Equipment: "barbell"}
	html := renderBrowse(t, []exercise.Exercise{illustrated()}, f, Page{
		Number: 1, Last: 3, Total: 60, FirstRow: 1,
	})

	// url.Values.Encode sorts keys, so the query string is deterministic.
	want := "category=strength&amp;equipment=barbell&amp;muscle=chest&amp;page=2&amp;q=bench+press"
	if !strings.Contains(html, want) {
		t.Errorf("the next-page link does not carry the filter; wanted %q", want)
	}
}

func TestPageOneURLOmitsThePageParameter(t *testing.T) {
	t.Parallel()

	if got := (Filters{}).pageURL(1); got != "/app/exercises" {
		t.Errorf("pageURL(1) = %q, want the bare path so page one has one URL", got)
	}
	if got := (Filters{Muscle: "lats"}).pageURL(1); got != "/app/exercises?muscle=lats" {
		t.Errorf("pageURL(1) with a filter = %q, want no page parameter", got)
	}
	if got := (Filters{Muscle: "lats"}).pageURL(2); got != "/app/exercises?muscle=lats&page=2" {
		t.Errorf("pageURL(2) = %q", got)
	}
}

func TestSummaryNamesTheRangeOnScreenRatherThanJustACount(t *testing.T) {
	t.Parallel()

	got := summary(24, Page{Number: 2, Last: 19, Total: 455, FirstRow: 25})
	if want := "Showing 25-48 of 455 exercises"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}

	// A result that fits on one page should not be described as a range.
	if got := summary(3, onePage(3)); got != "3 exercises" {
		t.Errorf("single-page summary = %q, want %q", got, "3 exercises")
	}
}

// The component namespaces its Alpine state by ID. Two rows sharing one would
// cycle as a single illustration.
func TestEveryIllustratedRowGetsItsOwnComponentID(t *testing.T) {
	t.Parallel()

	second := illustrated()
	second.Slug = "dumbbell-bench-press"
	second.IllustrationSlug = "dumbbell-bench-press"

	html := renderBrowse(t, []exercise.Exercise{illustrated(), second}, Filters{}, onePage(2))

	for _, slug := range []string{illustrated().Slug, second.Slug} {
		id := fmt.Sprintf(`id="art-row-%s"`, slug)
		if !strings.Contains(html, id) {
			t.Errorf("missing %s", id)
		}
	}
}

// The coach's transcript renders MusclePartial for an exercise it looked up.
// The artwork belongs there too — but a transcript can hold many of these, so
// the credit rides in the caption that already exists rather than adding a
// block per message.
func TestMusclePartialShowsTheIllustrationAndCreditsIt(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	if err := MusclePartial(illustrated()).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := b.String()

	if !strings.Contains(html, "/assets/exercises/bench-press/frame-1.svg") {
		t.Error("the coach partial shows no illustration")
	}
	if !strings.Contains(html, "creativecommons.org/licenses/by-sa/4.0") {
		t.Error("the coach partial shows artwork with no licence credit")
	}
	// The viewer must survive alongside it; the artwork adds to the answer
	// rather than replacing what the muscle model says.
	if !strings.Contains(html, "coach-muscles-"+illustrated().Slug) {
		t.Error("the muscle viewer disappeared when artwork was added")
	}
}

func TestMusclePartialWithoutArtworkStillRendersTheViewer(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	if err := MusclePartial(plainRow()).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := b.String()

	if strings.Contains(html, "/assets/exercises/") {
		t.Error("rendered artwork for an exercise that has none")
	}
	if !strings.Contains(html, "coach-muscles-"+plainRow().Slug) {
		t.Error("the muscle viewer did not render")
	}
}
