package coach

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
)

func known(refs ...string) map[string]bool {
	out := make(map[string]bool, len(refs))
	for _, r := range refs {
		out[r] = true
	}
	return out
}

func TestStripRefsRecordsOnlyOfferedRefs(t *testing.T) {
	offered := "memory:6f2c81a4-0000-4000-8000-000000000001"
	invented := "memory:deadbeef-0000-4000-8000-000000000009"

	reply := "You train fasted [[" + offered + "]] and you sleep badly [[" + invented + "]]."

	text, refs := StripRefs(reply, known(offered))

	if strings.Contains(text, "[[") {
		t.Errorf("citation survived into the visible reply: %q", text)
	}
	if want := "You train fasted and you sleep badly."; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
	if len(refs) != 1 || refs[0] != offered {
		// The invented ref must be stripped from the text like any other but
		// never recorded: an audit trail that includes facts the model was
		// never given is worse than no audit trail.
		t.Errorf("refs = %v, want [%s]", refs, offered)
	}
}

func TestStripRefsDeduplicates(t *testing.T) {
	ref := "memory:6f2c81a4-0000-4000-8000-000000000001"
	_, refs := StripRefs("a [["+ref+"]] b [["+ref+"]]", known(ref))
	if len(refs) != 1 {
		t.Errorf("refs = %v, want one entry", refs)
	}
}

func TestStripRefsLeavesOrdinaryBracketsAlone(t *testing.T) {
	reply := "Your log shows [see Tuesday] a missed session."
	text, refs := StripRefs(reply, known())
	if text != reply {
		t.Errorf("text = %q, want it unchanged", text)
	}
	if len(refs) != 0 {
		t.Errorf("refs = %v, want none", refs)
	}
}

func TestMemoryRefRoundTrips(t *testing.T) {
	id := uuid.New()
	kind, got, err := ParseRef(MemoryRef(id))
	if err != nil {
		t.Fatal(err)
	}
	if kind != EvidenceKindMemory || got != id.String() {
		t.Errorf("ParseRef = (%q, %q), want (%q, %q)", kind, got, EvidenceKindMemory, id)
	}
}

func TestParseRefRejectsUnknownKinds(t *testing.T) {
	for _, ref := range []string{"", "memory", "notes:abc", "memory:"} {
		if _, _, err := ParseRef(ref); err == nil {
			t.Errorf("ParseRef(%q) = nil error, want a failure", ref)
		}
	}
}

// The stream filter is the part that actually protects the reader: a citation
// arrives split across provider chunks, and stripping only at save time would
// show every handle in the browser before the reply finished.
func TestRefStripperHandlesSplitCitations(t *testing.T) {
	ref := "memory:6f2c81a4-0000-4000-8000-000000000001"
	full := "You train fasted [[" + ref + "]] most days."

	for _, size := range []int{1, 3, 7, 13, 64} {
		var (
			f   refStripper
			out strings.Builder
		)
		for _, chunk := range split(full, size) {
			out.WriteString(f.Take(chunk))
		}
		out.WriteString(f.Flush())

		got := out.String()
		if strings.Contains(got, "[[") || strings.Contains(got, ref) {
			t.Errorf("chunk size %d: citation reached the reader: %q", size, got)
		}
		if want := "You train fasted most days."; got != want {
			t.Errorf("chunk size %d: got %q, want %q", size, got, want)
		}
	}
}

// A bracket that never becomes a citation must not stall the stream forever.
func TestRefStripperReleasesUnclosedBrackets(t *testing.T) {
	var f refStripper
	long := "[" + strings.Repeat("x", maxRefLen+10)

	if got := f.Take(long); got == "" {
		t.Error("filter held back a bracket that cannot become a citation")
	}
}

func TestRefStripperFlushReturnsTheTail(t *testing.T) {
	var f refStripper
	if got := f.Take("done ["); got != "done" {
		t.Errorf("Take = %q, want the bracket and its space held back", got)
	}
	if got := f.Flush(); got != " [" {
		t.Errorf("Flush = %q, want the held bracket and space back", got)
	}
}

// A reply that simply ends in a space must not lose it.
func TestRefStripperHoldsThenReleasesATrailingSpace(t *testing.T) {
	var f refStripper
	var out strings.Builder

	out.WriteString(f.Take("two "))
	out.WriteString(f.Take("words"))
	out.WriteString(f.Flush())

	if got, want := out.String(), "two words"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExerciseLookupsAreRecordedOncePerSlug(t *testing.T) {
	calls := []ai.ToolCall{
		{Name: ToolGetExercise, Arguments: []byte(`{"slug":"barbell-back-squat"}`)},
		{Name: "search_exercises", Arguments: []byte(`{"query":"squat"}`)},
		{Name: ToolGetExercise, Arguments: []byte(`{"slug":"romanian-deadlift"}`)},
		// The model asking twice in one turn is normal and must not put the
		// same picture on the page twice.
		{Name: ToolGetExercise, Arguments: []byte(`{"slug":"barbell-back-squat"}`)},
	}

	got := appendExerciseLookups(nil, calls)
	want := []string{"barbell-back-squat", "romanian-deadlift"}

	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A malformed or empty argument costs the reply a picture, never the reply.
func TestExerciseLookupsSkipUnusableCalls(t *testing.T) {
	calls := []ai.ToolCall{
		{Name: ToolGetExercise, Arguments: []byte(`not json`)},
		{Name: ToolGetExercise, Arguments: []byte(`{"slug":"   "}`)},
		{Name: ToolGetExercise, Arguments: []byte(`{}`)},
	}

	if got := appendExerciseLookups(nil, calls); len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}

// Exercise refs are recorded, not cited: nothing should be cut out of a reply
// because one was stored against it.
func TestStripRefsLeavesExerciseHandlesAlone(t *testing.T) {
	reply := "Try the [[exercise:barbell-back-squat]] pattern."

	text, used := StripRefs(reply, known(ExerciseRef("barbell-back-squat")))
	if text != reply {
		t.Errorf("text = %q, want it untouched", text)
	}
	if len(used) != 0 {
		t.Errorf("used = %v, want none", used)
	}
}

func split(s string, size int) []string {
	var out []string
	for i := 0; i < len(s); i += size {
		out = append(out, s[i:min(i+size, len(s))])
	}
	return out
}
