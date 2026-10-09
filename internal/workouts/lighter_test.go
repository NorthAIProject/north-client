package workouts

import "testing"

func TestVolumeOnALighterDay(t *testing.T) {
	w := WeekProgress{Volume: VolumeBuild, LighterToday: "Wednesday"}
	if got := w.VolumeOn("wednesday"); got != VolumeDeload {
		t.Fatalf("today = %q, want deload", got)
	}
	if got := w.VolumeOn("Thursday"); got != VolumeBuild {
		t.Fatalf("another day = %q, want the week's build", got)
	}
	if got := (WeekProgress{Volume: VolumeHold}).VolumeOn("Wednesday"); got != VolumeHold {
		t.Fatalf("no lighter day = %q", got)
	}
}
