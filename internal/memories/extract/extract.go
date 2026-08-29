// Package extract holds the structured shape of memory extraction and the
// rules that keep the model from inventing facts about a person.
package extract

import (
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/memories/memory"
)

// Result is what the model must return.
type Result struct {
	Facts []Candidate `json:"facts"`
}

// Candidate is one proposed durable fact before it becomes a pending memory.
type Candidate struct {
	Category   string  `json:"category"`
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`

	// Supersedes is the 1-based number of the believed fact this one replaces,
	// or 0 for a genuinely new fact.
	//
	// An index into a list rendered into the prompt, not an identifier. Asking a
	// model to echo a uuid back is asking it to copy 36 characters without a
	// typo, and a mistyped uuid resolves to nothing — the supersession silently
	// does not happen and two contradicting facts stay in the store, which is
	// the exact failure this feature exists to prevent. A small integer either
	// lands in range or is rejected.
	Supersedes int `json:"supersedes"`
}

const (
	minContentLen = 8
	maxContentLen = 240
	minConfidence = 0.55
	maxFacts      = 5
)

// Schema is the shape the model must return.
func Schema() *ai.Schema {
	fact := ai.Object("one durable fact about the person", map[string]*ai.Schema{
		"category":   ai.Enum("kind of fact", memory.Categories...),
		"content":    ai.String("short durable fact; only what they clearly stated"),
		"confidence": ai.Number("0 to 1 how sure you are they actually said this"),
		"supersedes": ai.Number("number of the believed fact this replaces, or 0 for a new fact"),
	}, "category", "content", "confidence", "supersedes")
	return ai.Object("facts extracted from the conversation", map[string]*ai.Schema{
		"facts": ai.Array("zero or more durable facts; empty is preferred over guessing", fact),
	}, "facts")
}

// Sanitise drops weak, empty, or out-of-policy candidates. Empty results are
// success: the model often has nothing durable to learn.
//
// offered is how many believed facts the prompt numbered. A Supersedes value
// outside 1..offered is dropped to 0 rather than rejecting the whole fact: the
// content may be perfectly good and the index merely invented, and losing a real
// fact over a bad pointer would be the worse trade. The consequence is a
// duplicate for a human to resolve, which is visible, rather than a wrong
// retirement, which is not.
func Sanitise(in Result, offered int) []Candidate {
	out := make([]Candidate, 0, len(in.Facts))
	seen := map[string]bool{}

	for _, f := range in.Facts {
		if len(out) >= maxFacts {
			break
		}
		c := Candidate{
			Category:   strings.TrimSpace(f.Category),
			Content:    strings.TrimSpace(f.Content),
			Confidence: f.Confidence,
		}
		if c.Category == "" {
			c.Category = memory.CategoryGeneral
		}
		if !validCategory(c.Category) {
			continue
		}
		if len(c.Content) < minContentLen || len(c.Content) > maxContentLen {
			continue
		}
		if c.Confidence < minConfidence || c.Confidence > 1 {
			continue
		}
		// Daily mood chatter is a check-in, not a memory.
		if looksLikeEphemeralMood(c.Content) {
			continue
		}
		if f.Supersedes >= 1 && f.Supersedes <= offered {
			c.Supersedes = f.Supersedes
		}

		key := strings.ToLower(c.Content)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

func validCategory(c string) bool {
	for _, known := range memory.Categories {
		if c == known {
			return true
		}
	}
	return false
}

func looksLikeEphemeralMood(s string) bool {
	lower := strings.ToLower(s)
	needles := []string{
		"feeling good today",
		"feeling bad today",
		"tired today",
		"mood today",
		"energy today",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}
