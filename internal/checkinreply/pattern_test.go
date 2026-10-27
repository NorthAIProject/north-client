package checkinreply

import (
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
)

func entry(mood, energy int, sleep, stress *int, tags ...string) checkin.CheckIn {
	return checkin.CheckIn{Mood: mood, Energy: energy, SleepQuality: sleep, Stress: stress, Tags: tags}
}

func n(v int) *int { return &v }

func repeat(times int, c checkin.CheckIn) []checkin.CheckIn {
	out := make([]checkin.CheckIn, times)
	for i := range out {
		out[i] = c
	}
	return out
}

func concat(parts ...[]checkin.CheckIn) []checkin.CheckIn {
	var out []checkin.CheckIn
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestStrongestPattern(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		list []checkin.CheckIn
		want []string // substrings; nil means no pattern
	}{
		{
			name: "sleep and energy",
			list: concat(repeat(3, entry(3, 5, n(5), nil)), repeat(3, entry(3, 2, n(1), nil))),
			want: []string{"sleep", "energy", "5.0", "2.0"},
		},
		{
			name: "stress and mood",
			list: concat(repeat(3, entry(2, 3, nil, n(5))), repeat(4, entry(4, 3, nil, n(1)))),
			want: []string{"stress", "mood", "2.0", "4.0"},
		},
		{
			name: "a frequent tag and mood",
			list: concat(repeat(3, entry(2, 3, nil, nil, "travel")), repeat(3, entry(4, 3, nil, nil))),
			want: []string{`"travel"`, "mood", "2.0", "4.0"},
		},
		{
			// The biggest difference is the one worth a sentence.
			name: "the strongest wins",
			list: concat(
				repeat(3, entry(4, 5, n(5), nil, "work")),
				repeat(3, entry(3, 1, n(1), nil)),
			),
			want: []string{"sleep", "energy"},
		},
		{
			name: "two samples on a side are not a pattern",
			list: concat(repeat(2, entry(3, 5, n(5), nil)), repeat(3, entry(3, 2, n(1), nil))),
		},
		{
			name: "a small gap is not a pattern",
			list: concat(repeat(3, entry(3, 4, n(5), nil)), repeat(2, entry(3, 3, n(1), nil)),
				repeat(2, entry(3, 4, n(1), nil))),
		},
		{name: "nothing rated", list: repeat(10, entry(3, 3, nil, nil))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := strongestPattern(tc.list)
			if tc.want == nil {
				if ok {
					t.Fatalf("unexpected pattern %q", got)
				}
				return
			}
			if !ok {
				t.Fatal("expected a pattern")
			}
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("pattern %q missing %q", got, s)
				}
			}
		})
	}
}
