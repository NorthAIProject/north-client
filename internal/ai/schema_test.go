package ai_test

import (
	"slices"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// A provider caches a request by its exact bytes, tools first. A required list
// built by ranging over a map comes out in a different order each call, so the
// tools block never repeats and the cache never hits.
func TestADerivedRequiredListIsTheSameEveryTime(t *testing.T) {
	schema := &ai.Schema{Type: ai.TypeObject, Properties: map[string]*ai.Schema{
		"slug": ai.String("s"), "muscle": ai.String("m"), "equipment": ai.String("e"),
		"query": ai.String("q"), "limit": ai.Integer("l"), "day": ai.String("d"),
	}}

	want := []string{"day", "equipment", "limit", "muscle", "query", "slug"}
	for range 50 {
		got, _ := ai.JSONSchema(schema)["required"].([]string)
		if !slices.Equal(got, want) {
			t.Fatalf("required = %v, want %v every time", got, want)
		}
	}
}
