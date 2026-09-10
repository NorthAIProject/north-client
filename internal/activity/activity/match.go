package activity

import (
	"sort"
	"strings"
)

// Match resolves a spoken activity name to a MET entry.
//
// A person says "run" or "went for a swim", not "running_9_8kmh". The coach
// and the log form both need the same answer to the same words, so the rule
// lives here in the leaf rather than in either caller.
//
// The result is either one entry, or nothing plus the candidates it could
// have meant. A caller that gets candidates should say so and list them: a
// model recovers in one turn when the refusal names the choices.
//
// speedKmh is the pace when the caller knows it, and picks the running entry
// for someone who said "run" — the table splits running by pace, and the
// person who ran 5 km in 30 minutes has already told us which one.
func Match(name string, speedKmh float64) (MET, []MET) {
	query := strings.ToLower(strings.TrimSpace(name))
	if query == "" {
		return MET{}, nil
	}

	if met, ok := LookupMET(query); ok {
		return met, nil
	}
	for _, met := range METTable {
		if strings.ToLower(met.Name) == query {
			return met, nil
		}
	}

	tokens := meaningfulTokens(query)
	if len(tokens) == 0 {
		return MET{}, nil
	}

	var candidates []MET
	for _, met := range METTable {
		if containsAll(strings.ToLower(met.Name), tokens) {
			candidates = append(candidates, met)
		}
	}

	switch len(candidates) {
	case 0:
		return MET{}, nil
	case 1:
		return candidates[0], nil
	}

	if speedKmh > 0 && allRunning(candidates) {
		return runningForSpeed(speedKmh), nil
	}

	if met, ok := defaultOfFamily(candidates); ok {
		return met, nil
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Code < candidates[j].Code })
	return MET{}, candidates
}

// synonyms folds the words people use onto the words the table uses. Only
// the verbs and nouns that do not already appear in a MET name belong here.
var synonyms = map[string]string{
	"run": "running", "ran": "running", "runs": "running",
	"jog": "running", "jogging": "running", "jogged": "running",
	"walk": "walking", "walked": "walking", "walks": "walking",
	"hike": "hiking", "hiked": "hiking",
	"bike": "cycling", "biking": "cycling", "biked": "cycling",
	"cycle": "cycling", "cycled": "cycling", "ride": "cycling", "rode": "cycling",
	"swim": "swimming", "swam": "swimming",
	"row": "rowing", "rowed": "rowing",
	"lift": "strength", "lifting": "strength", "lifted": "strength",
	"weights": "strength", "gym": "strength",
	"stretch": "stretching", "stretched": "stretching",
	"climb": "climbing", "climbed": "climbing",
	"dance": "dancing", "danced": "dancing",
	"ski": "skiing", "skied": "skiing",
	"box": "boxing", "boxed": "boxing",
	"football": "soccer",
}

// stopWords are the parts of "went for a run" that are not the run.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "for": true, "went": true, "go": true,
	"did": true, "do": true, "some": true, "my": true, "of": true, "session": true,
	"workout": true, "training": true, "and": true, "with": true,
}

func meaningfulTokens(query string) []string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == ',' || r == '-' || r == '/' || r == '(' || r == ')'
	})

	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		if stopWords[f] {
			continue
		}
		if s, ok := synonyms[f]; ok {
			f = s
		}
		tokens = append(tokens, f)
	}
	return tokens
}

func containsAll(haystack string, tokens []string) bool {
	for _, t := range tokens {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}

func allRunning(candidates []MET) bool {
	for _, c := range candidates {
		if !strings.HasPrefix(c.Code, "running_") {
			return false
		}
	}
	return true
}

// runningForSpeed picks the running entry nearest a pace. The boundaries sit
// halfway between the table's own speeds, so 9 km/h reads as the 8 km/h entry
// and 10 km/h as the 9.8 one.
func runningForSpeed(kmh float64) MET {
	var code string
	switch {
	case kmh < 8.9:
		code = "running_8kmh"
	case kmh < 10.55:
		code = "running_9_8kmh"
	case kmh < 12.15:
		code = "running_11_3kmh"
	default:
		code = "running_fast"
	}
	met, _ := LookupMET(code) // the four codes above are in the table
	return met
}

// defaultOfFamily picks the entry a person most likely means when they name
// a family without an intensity: "strength training" is the general entry,
// "swimming" the moderate one. Nothing is chosen when the candidates span
// more than one family, or when the family has no obvious middle.
func defaultOfFamily(candidates []MET) (MET, bool) {
	label := sportLabel(candidates[0].Code)
	for _, c := range candidates[1:] {
		if sportLabel(c.Code) != label {
			return MET{}, false
		}
	}

	for _, suffix := range []string{"(general)", "(moderate"} {
		for _, c := range candidates {
			if strings.Contains(c.Name, suffix) {
				return c, true
			}
		}
	}
	return MET{}, false
}
