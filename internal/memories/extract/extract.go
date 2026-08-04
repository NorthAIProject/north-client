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
	}, "category", "content", "confidence")
	return ai.Object("facts extracted from the conversation", map[string]*ai.Schema{
		"facts": ai.Array("zero or more durable facts; empty is preferred over guessing", fact),
	}, "facts")
}

// Sanitise drops weak, empty, or out-of-policy candidates. Empty results are
// success: the model often has nothing durable to learn.
func Sanitise(in Result) []Candidate {
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
