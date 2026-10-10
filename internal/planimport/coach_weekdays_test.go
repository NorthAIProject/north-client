package planimport

import (
	"strings"
	"testing"
	"time"
)

func TestSpreadWeekdaysSpacesTrainingDaysOverTheWeek(t *testing.T) {
	for n, want := range map[int]string{
		0: "", 1: "Mon", 2: "Mon,Thu", 3: "Mon,Wed,Fri", 4: "Mon,Tue,Thu,Fri",
		5: "Mon,Tue,Wed,Thu,Fri", 6: "Mon,Tue,Wed,Thu,Fri,Sat", 7: "Mon,Tue,Wed,Thu,Fri,Sat,Sun", 8: "",
	} {
		if got := dayNames(spreadWeekdays(n)); got != want {
			t.Errorf("spreadWeekdays(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestFreeWeekdaysPrefersAndSkipsTakenDays(t *testing.T) {
	for _, tc := range []struct {
		name   string
		n      int
		taken  string
		prefer string
		want   string
	}{
		{"no preference fills Monday onwards", 3, "", "", "Mon,Tue,Wed"},
		{"a taken day moves on to the next free one", 2, "Mon", "Mon,Thu", "Thu,Tue"},
		{"preferred days first, in their order", 2, "", "Sat,Tue", "Sat,Tue"},
		{"a taken preferred day is skipped", 2, "Tue", "Tue,Sat", "Sat,Mon"},
		{"the week running out gives fewer", 3, "Mon,Tue,Wed,Thu,Fri,Sat", "", "Sun"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dayNames(freeWeekdays(tc.n, shortDays(t, tc.taken), shortDays(t, tc.prefer))); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func dayNames(days []time.Weekday) string {
	names := make([]string, len(days))
	for i, d := range days {
		names[i] = d.String()[:3]
	}
	return strings.Join(names, ",")
}

func shortDays(t *testing.T, list string) []time.Weekday {
	t.Helper()
	var out []time.Weekday
	for _, s := range strings.Split(list, ",") {
		if s == "" {
			continue
		}
		d, ok := weekdayOf(s)
		if !ok {
			t.Fatalf("not a weekday: %q", s)
		}
		out = append(out, d)
	}
	return out
}
