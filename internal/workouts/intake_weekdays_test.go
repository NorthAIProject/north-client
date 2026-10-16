package workouts

import (
	"strings"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestValidateIntakeChecksTheChosenWeekdays(t *testing.T) {
	t.Parallel()

	base := Intake{Goal: "strength", DaysPerWeek: 3, SessionMinutes: 45}
	for _, tc := range []struct {
		weekdays []string
		want     string
	}{
		{nil, ""},
		{[]string{"friday", "Monday", "Wednesday"}, ""},
		{[]string{"Monday", "Wednesday"}, "Choose exactly 3 weekdays"},
		{[]string{"Monday", "Monday", "Friday"}, "at most once"},
		{[]string{"Monday", "Funday", "Friday"}, "at most once"},
	} {
		in := base
		in.Weekdays = tc.weekdays
		err := ValidateIntake(in)
		var fields apperr.FieldErrors
		got := ""
		if apperr.As(err, &fields) {
			got = fields.Messages()["weekdays"]
		}
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("weekdays %q: error %q, want %q", tc.weekdays, got, tc.want)
		}
	}

	in := base
	in.Weekdays = []string{"friday", "Monday", "wednesday"}
	if got := normalizeIntake(in).Weekdays; strings.Join(got, ",") != "Monday,Wednesday,Friday" {
		t.Fatalf("normalized = %q", got)
	}
	if req := intakeRequest(normalizeIntake(in)); !strings.Contains(req, "Those days are Monday, Wednesday, Friday") {
		t.Fatalf("the request does not name the days:\n%s", req)
	}
}
