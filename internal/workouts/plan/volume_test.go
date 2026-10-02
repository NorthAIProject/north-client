package plan

import "testing"

func TestVolumeSetsFor(t *testing.T) {
	cases := []struct {
		v           Volume
		index, sets int
		want        int
	}{
		{VolumeHold, 0, 4, 4},
		{"", 0, 4, 4},
		{VolumeDeload, 0, 4, 2},
		{VolumeDeload, 3, 5, 3},
		{VolumeDeload, 0, 1, 1},
		{VolumeBuild, 0, 3, 4},
		{VolumeBuild, 1, 3, 4},
		{VolumeBuild, 2, 3, 3},
	}
	for _, c := range cases {
		if got := c.v.SetsFor(c.index, c.sets); got != c.want {
			t.Errorf("%q.SetsFor(%d, %d) = %d, want %d", c.v, c.index, c.sets, got, c.want)
		}
	}
}

func TestVolumeDayLeavesThePlanAlone(t *testing.T) {
	day := PlanDay{Weekday: "Monday", Exercises: []Exercise{{Name: "Squat", Sets: 5}, {Name: "Row", Sets: 3}}}
	got := VolumeDeload.Day(day)
	if got.Exercises[0].Sets != 3 || got.Exercises[1].Sets != 2 {
		t.Fatalf("deload day = %+v", got.Exercises)
	}
	if day.Exercises[0].Sets != 5 {
		t.Fatalf("the plan was changed: %+v", day.Exercises)
	}
}
