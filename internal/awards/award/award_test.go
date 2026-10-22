package award_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func TestCatalogIsTheSpecTable(t *testing.T) {
	cat := award.Catalog()
	if len(cat) != 31 {
		t.Fatalf("catalog has %d badges, want 31", len(cat))
	}
	seen := map[string]bool{}
	for _, b := range cat {
		if seen[b.ID] {
			t.Errorf("duplicate id %s", b.ID)
		}
		seen[b.ID] = true
		if b.Target < 1 || b.Glyph == "" || b.Area == "" {
			t.Errorf("%s is incomplete: %+v", b.ID, b)
		}
		got, ok := award.ByID(b.ID)
		if !ok || got != b {
			t.Errorf("ByID(%s) = %+v, %v", b.ID, got, ok)
		}
	}
	if _, ok := award.ByID("nope"); ok {
		t.Error("ByID found a badge that does not exist")
	}
	private := map[award.Area]bool{
		award.Nutrition: true, award.Hydration: true, award.Sleep: true, award.Mind: true, award.Body: true,
	}
	areas := []award.Area{
		award.Training, award.Lifts, award.Consistency, award.Habits, award.Nutrition, award.Hydration,
		award.Sleep, award.Mind, award.Goals, award.Body, award.Social,
	}
	for _, a := range areas {
		if a.Private() != private[a] {
			t.Errorf("%s.Private() = %v, want %v", a, a.Private(), private[a])
		}
	}
	for id, target := range map[string]int{"workouts_100": 100, "early_bird": 10, "streak_365": 365, "habit_locked_in": 4, "reviews_8": 8, "checks_4": 4} {
		if b, _ := award.ByID(id); b.Target != target {
			t.Errorf("%s target = %d, want %d", id, b.Target, target)
		}
	}
}

func TestEveryBadgeHasCopyInEveryLocale(t *testing.T) {
	for _, locale := range []string{"en", "pt-PT", "pt-BR", "es"} {
		for _, b := range award.Catalog() {
			for _, key := range []string{"awards.badge." + b.ID + ".name", "awards.badge." + b.ID + ".desc", "awards.area." + string(b.Area)} {
				if i18n.Translate(locale, key) == key {
					t.Errorf("%s has no %s", locale, key)
				}
			}
		}
	}
}
