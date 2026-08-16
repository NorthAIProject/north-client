package eval

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// Case is one grounding scenario, graded at two depths.
//
// The same fixture answers two different questions. Offline, with no provider
// and no database, the Prompt assertions ask whether the facts reached the
// model at all. Live, against a real provider, the Reply assertions ask what
// the model did with them. Keeping both on one Case is what stops the two
// tiers from drifting apart: there is one definition of "the user has two
// goals and a sore knee", and both tiers read it.
type Case struct {
	// ID names the case in test output. Kebab-case, because it becomes the
	// subtest name and `go test -run` is easier to type without spaces.
	ID string

	// Why is one line on what breaking this case would cost the user. It is
	// printed on failure, so whoever sees the red does not have to reconstruct
	// the intent from the assertions.
	Why string

	// Context is the fixture, rendered through the real coach.PromptBuilder so
	// the evals grade the format production actually sends.
	Context *coach.Context

	// Ask is the user's message for the live tier.
	Ask string

	Prompt []PromptAssertion
	Reply  []ReplyAssertion
}

// PromptAssertion grades the assembled system prompt. Deterministic: no
// provider, no network, no database.
type PromptAssertion interface {
	Name() string
	Check(system string, cc *coach.Context) error
}

// ReplyAssertion grades what a model wrote back.
type ReplyAssertion interface {
	Name() string
	Check(reply string, cc *coach.Context) error
}

// GradePrompt runs the prompt assertions, returning one message per failure.
//
// Returns strings rather than calling t.Errorf so this file stays free of
// testing: the same grading is used by an offline test, a live test, and
// potentially a command that reports a score.
func (c Case) GradePrompt(system string) []string {
	var out []string
	for _, a := range c.Prompt {
		if err := a.Check(system, c.Context); err != nil {
			out = append(out, fmt.Sprintf("assertion %s: %v", a.Name(), err))
		}
	}
	return out
}

// GradeReply runs the reply assertions, returning one message per failure.
func (c Case) GradeReply(reply string) []string {
	var out []string
	for _, a := range c.Reply {
		if err := a.Check(reply, c.Context); err != nil {
			out = append(out, fmt.Sprintf("assertion %s: %v", a.Name(), err))
		}
	}
	return out
}

// promptCheck adapts a function to PromptAssertion, so a new assertion is a
// constructor rather than a new type with two methods.
type promptCheck struct {
	name  string
	check func(system string, cc *coach.Context) error
}

func (p promptCheck) Name() string { return p.name }

func (p promptCheck) Check(system string, cc *coach.Context) error {
	return p.check(system, cc)
}

type replyCheck struct {
	name  string
	check func(reply string, cc *coach.Context) error
}

func (r replyCheck) Name() string { return r.name }

func (r replyCheck) Check(reply string, cc *coach.Context) error {
	return r.check(reply, cc)
}

// Renders requires every want to appear in the system prompt, verbatim.
//
// Case-sensitive on purpose. These are the prompt's own headings and the
// domain's own Summary() output; matching them loosely would let a renaming of
// "Known about them" pass unnoticed, which is the drift this harness exists to
// catch.
func Renders(want ...string) PromptAssertion {
	return promptCheck{
		name: "Renders",
		check: func(system string, _ *coach.Context) error {
			var missing []string
			for _, w := range want {
				if !strings.Contains(system, w) {
					missing = append(missing, w)
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("the prompt is missing %s", quoteAll(missing))
			}
			return nil
		},
	}
}

// DoesNotRender requires none of the unwanted strings to appear in the prompt.
func DoesNotRender(unwanted ...string) PromptAssertion {
	return promptCheck{
		name: "DoesNotRender",
		check: func(system string, _ *coach.Context) error {
			var found []string
			for _, u := range unwanted {
				if strings.Contains(system, u) {
					found = append(found, u)
				}
			}
			if len(found) > 0 {
				return fmt.Errorf("the prompt contains %s, which it must not", quoteAll(found))
			}
			return nil
		},
	}
}

// RendersInOrder requires first to appear before second.
//
// Ordering carries meaning in the context block: a pinned fact is placed ahead
// of the rest because it claims the character budget first, and a model reading
// a long list weights the top of it.
func RendersInOrder(first, second string) PromptAssertion {
	return promptCheck{
		name: "RendersInOrder",
		check: func(system string, _ *coach.Context) error {
			i, j := strings.Index(system, first), strings.Index(system, second)
			switch {
			case i < 0:
				return fmt.Errorf("the prompt is missing %q", first)
			case j < 0:
				return fmt.Errorf("the prompt is missing %q", second)
			case i > j:
				return fmt.Errorf("%q should come before %q, but it comes after", first, second)
			}
			return nil
		},
	}
}

// Mentions requires every want to appear in the reply, case-insensitively.
//
// Model output is graded on substance rather than casing: a coach that writes
// "Squatted 120kg" has used the fact just as well as one that writes "squatted".
func Mentions(want ...string) ReplyAssertion {
	return replyCheck{
		name: "Mentions",
		check: func(reply string, _ *coach.Context) error {
			lower := strings.ToLower(reply)
			var missing []string
			for _, w := range want {
				if !strings.Contains(lower, strings.ToLower(w)) {
					missing = append(missing, w)
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("the reply never mentions %s, though the context supplied it", quoteAll(missing))
			}
			return nil
		},
	}
}

// DoesNotMention requires none of the unwanted strings to appear in the reply.
func DoesNotMention(unwanted ...string) ReplyAssertion {
	return replyCheck{
		name: "DoesNotMention",
		check: func(reply string, _ *coach.Context) error {
			lower := strings.ToLower(reply)
			var found []string
			for _, u := range unwanted {
				if strings.Contains(lower, strings.ToLower(u)) {
					found = append(found, u)
				}
			}
			if len(found) > 0 {
				return fmt.Errorf("the reply mentions %s, which it was never given", quoteAll(found))
			}
			return nil
		},
	}
}

// ignorancePhrases are the ways a model admits it was not told something.
//
// A list rather than one phrase because a refusal is correct however it is
// worded, and grading on exact wording would fail a good reply for being
// polite differently than expected.
var ignorancePhrases = []string{
	"don't have", "do not have", "haven't", "have not",
	"don't know", "do not know", "no record", "not recorded",
	"you haven't told me", "first conversation", "nothing recorded",
	"don't see", "do not see",
}

// AdmitsIgnorance requires the reply to concede it lacks the information.
//
// The central claim of the product. A model that fabricates a squat max would
// fabricate an injury history too, and the coaching would be actively
// dangerous.
func AdmitsIgnorance() ReplyAssertion {
	return replyCheck{
		name: "AdmitsIgnorance",
		check: func(reply string, _ *coach.Context) error {
			lower := strings.ToLower(reply)
			for _, p := range ignorancePhrases {
				if strings.Contains(lower, p) {
					return nil
				}
			}
			// Print the phrases: without them, whoever reads the failure
			// cannot tell whether the model was wrong or the list is too narrow.
			return fmt.Errorf("the reply never admits it lacks the information; looked for any of %s",
				quoteAll(ignorancePhrases))
		},
	}
}

// refPattern matches a citation as the model writes it.
//
// Deliberately a separate pattern from coach's refRemoval, which is unexported
// and also swallows the space before a citation because it is repairing text
// for a reader. Here we only want to know which refs were written.
var refPattern = regexp.MustCompile(`\[\[(memory|chunk):([A-Za-z0-9_-]{1,80})\]\]`)

// CitesOnlyOfferedRefs requires every citation in the reply to be one the
// context actually offered.
//
// This is the RAG invariant. coach.StripRefs drops an invented ref without
// recording it, so in production an invention is invisible by design — the
// audit trail stays honest but nobody learns the model is doing it. Here we
// read the raw reply, where the invention is still visible, and fail on it.
func CitesOnlyOfferedRefs() ReplyAssertion {
	return replyCheck{
		name: "CitesOnlyOfferedRefs",
		check: func(reply string, cc *coach.Context) error {
			offered := cc.OfferedRefs()

			var invented []string
			for _, m := range refPattern.FindAllStringSubmatch(reply, -1) {
				ref := m[1] + ":" + m[2]
				if !offered[ref] {
					invented = append(invented, ref)
				}
			}
			if len(invented) > 0 {
				return fmt.Errorf("the reply cited %s, which was never offered; offered were %s",
					quoteAll(invented), quoteAll(sortedKeys(offered)))
			}
			return nil
		},
	}
}

// quoteAll renders a list for a failure message.
func quoteAll(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, fmt.Sprintf("%q", s))
	}
	return strings.Join(quoted, ", ")
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
