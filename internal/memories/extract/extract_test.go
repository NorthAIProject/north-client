package extract_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/memories/memory"
)

func TestSanitiseDropsWeakAndKeepsStrong(t *testing.T) {
	t.Parallel()

	got := extract.Sanitise(extract.Result{Facts: []extract.Candidate{
		{Category: memory.CategoryHabit, Content: "Prefers morning runs before work", Confidence: 0.9},
		{Category: memory.CategoryInjury, Content: "hi", Confidence: 0.9},                   // too short
		{Category: memory.CategoryInjury, Content: "Bad knee from skiing", Confidence: 0.2}, // low conf
		{Category: "nonsense", Content: "Has a dog named Max", Confidence: 0.9},             // bad category
		{Category: memory.CategoryHabit, Content: "Feeling good today", Confidence: 0.9},    // ephemeral
		{Category: memory.CategoryEquipment, Content: "Only owns a pair of dumbbells", Confidence: 0.8},
	}})

	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(got), got)
	}
	if got[0].Content != "Prefers morning runs before work" {
		t.Fatalf("first = %q", got[0].Content)
	}
	if got[1].Content != "Only owns a pair of dumbbells" {
		t.Fatalf("second = %q", got[1].Content)
	}
}

func TestSanitiseEmptyIsOK(t *testing.T) {
	t.Parallel()
	if got := extract.Sanitise(extract.Result{}); len(got) != 0 {
		t.Fatalf("empty input should stay empty, got %+v", got)
	}
}

func TestSanitiseCapsAtFive(t *testing.T) {
	t.Parallel()
	var facts []extract.Candidate
	for i := 0; i < 10; i++ {
		facts = append(facts, extract.Candidate{
			Category:   memory.CategoryGeneral,
			Content:    "Durable fact number " + string(rune('a'+i)) + " about the person clearly",
			Confidence: 0.9,
		})
	}
	// Make contents unique with enough length.
	for i := range facts {
		facts[i].Content = "Durable fact number " + string(rune('a'+i)) + " about the person clearly stated"
	}
	got := extract.Sanitise(extract.Result{Facts: facts})
	if len(got) != 5 {
		t.Fatalf("got %d, want 5", len(got))
	}
}

func TestSchemaIsNonNil(t *testing.T) {
	t.Parallel()
	if extract.Schema() == nil {
		t.Fatal("schema required")
	}
}
