package aiconsent

import (
	"reflect"
	"testing"
)

func TestRecipientsNamesCompaniesAndDropsTheTestModel(t *testing.T) {
	got := Recipients([]string{"openrouter", "fake", "anthropic", "nvidia"})
	want := []string{"Anthropic", "NVIDIA", "OpenRouter"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
