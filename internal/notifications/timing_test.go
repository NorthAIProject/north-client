package notifications

import "testing"

// An app built before the timing fields sends none of them, and must not
// reset a saved 06:00 briefing to midnight.
func TestKeepingFillsOnlyWhatIsMissing(t *testing.T) {
	saved := Prefs{BriefingHour: 6, EveningReflection: true, EveningHour: 22}
	hour := 8

	got := Input{BriefingHour: &hour}.Keeping(saved)
	if *got.BriefingHour != 8 || !*got.EveningReflection || *got.EveningHour != 22 {
		t.Fatalf("got briefing %d, reflection %v, evening %d", *got.BriefingHour, *got.EveningReflection, *got.EveningHour)
	}
}

func TestValidateRefusesHoursOutsideTheDay(t *testing.T) {
	for _, hour := range []int{-1, 24} {
		h := hour
		if _, err := Validate(Input{BriefingHour: &h}); err == nil {
			t.Fatalf("briefing hour %d accepted", hour)
		}
		if _, err := Validate(Input{EveningHour: &h}); err == nil {
			t.Fatalf("evening hour %d accepted", hour)
		}
	}
	for _, hour := range []int{0, 23} {
		h := hour
		if _, err := Validate(Input{BriefingHour: &h, EveningHour: &h}); err != nil {
			t.Fatalf("hour %d refused: %v", hour, err)
		}
	}
}
