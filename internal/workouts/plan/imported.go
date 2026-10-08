package plan

import (
	"fmt"
	"strings"
)

// ValidateImported checks a plan read from a person's own file.
//
// Deliberately looser than Validate. That one holds a model to what the person
// asked for; this one holds a file to what can be stored and trained from.
// There is no intake to compare against, no rationale to require, and a set
// count the file left out stays zero — "not stated" — rather than failing the
// import or being made up. What remains is what every screen relies on: a
// name, real weekdays, one session per weekday, and exercises with names.
func ValidateImported(p Plan) []string {
	var problems []string

	if strings.TrimSpace(p.Name) == "" {
		problems = append(problems, "the plan has no name")
	}
	if len(p.Days) == 0 {
		problems = append(problems, "the plan has no training days")
	}
	if len(p.Days) > 7 {
		problems = append(problems, fmt.Sprintf("the plan has %d training days but a week has 7", len(p.Days)))
	}

	seen := map[string]bool{}
	for i, day := range p.Days {
		wd, ok := parseWeekday(day.Weekday)
		if !ok {
			problems = append(problems, fmt.Sprintf("training day %d has no day of the week", i+1))
		} else if seen[wd.String()] {
			problems = append(problems, fmt.Sprintf("%s is used for more than one training day", wd))
		} else {
			seen[wd.String()] = true
		}

		if len(day.Exercises) == 0 {
			problems = append(problems, fmt.Sprintf("%s has no exercises", dayLabel(day)))
		}
		for _, ex := range day.Exercises {
			if strings.TrimSpace(ex.Name) == "" {
				problems = append(problems, fmt.Sprintf("%s contains an exercise with no name", dayLabel(day)))
			}
			if ex.Sets < 0 {
				problems = append(problems, fmt.Sprintf("%q has %d sets", ex.Name, ex.Sets))
			}
			if ex.RestSeconds < 0 {
				problems = append(problems, fmt.Sprintf("%q has negative rest", ex.Name))
			}
		}
	}

	return problems
}
