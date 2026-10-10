package strava

import (
	"encoding/json"
	"testing"
)

// Strava's list payload carries heart rate and the trainer flag beside the
// fields North already read. They decode, and where a session happened is
// inside on a trainer or virtual sport, outside with a GPS route, and
// unknown for a manual entry with neither.
func TestStravaActivityReadsHeartRateAndWhere(t *testing.T) {
	t.Parallel()
	raw := `{"id":1,"sport_type":"Run","average_heartrate":151.4,"max_heartrate":178,"trainer":false,
	         "map":{"summary_polyline":"abc"}}`
	var a apiActivity
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	if a.AverageHeartrate != 151.4 || a.MaxHeartrate != 178 {
		t.Errorf("heart rate %v / %v", a.AverageHeartrate, a.MaxHeartrate)
	}

	cases := []struct {
		name string
		a    apiActivity
		want *bool
	}{
		{"route outside", a, ptr(false)},
		{"treadmill flag", apiActivity{SportType: "Run", Trainer: true}, ptr(true)},
		{"virtual ride", apiActivity{SportType: "VirtualRide"}, ptr(true)},
		{"manual entry, no route", apiActivity{SportType: "Run"}, nil},
	}
	for _, c := range cases {
		got := c.a.where()
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: where = %v, want %v", c.name, show(got), show(c.want))
		}
	}
}

func ptr(b bool) *bool { return &b }

func show(b *bool) string {
	if b == nil {
		return "unknown"
	}
	if *b {
		return "inside"
	}
	return "outside"
}
