package dailyposts

import (
	"regexp"
	"strings"
)

// denied are phrasings a daily post must never contain, whatever the
// reviewer model thinks of them. The post reaches everyone with no context
// about their health, so anything that reads as a dose, a treatment, a cure or
// a diet target is out. Cheap and blunt on purpose: a false positive costs a
// retry, a false negative is advice someone might follow.
var denied = []struct {
	reason string
	re     *regexp.Regexp
}{
	{"dosage", regexp.MustCompile(`(?i)\b\d+(\.\d+)?\s?(mg|mcg|µg|iu|milligrams?|micrograms?)\b`)},
	{"calorie target", regexp.MustCompile(`(?i)\b\d{3,4}\s?(k?cal|calories)\b`)},
	{"supplement", regexp.MustCompile(`(?i)\bsupplement(s|ation)?\b`)},
	{"medication", regexp.MustCompile(`(?i)\b(medication|medicine|prescription|drug)s?\b`)},
	{"cure", regexp.MustCompile(`(?i)\b(cures?|curing|treatment|(treats?|prevents?|reverses?) (anxiety|depression|insomnia|disease|illness|pain|diabetes|cancer))\b`)},
	{"diagnosis", regexp.MustCompile(`(?i)\bdiagnos(e|es|is|ed)\b`)},
	{"detox", regexp.MustCompile(`(?i)\b(detox|cleanse|toxins?)\b`)},
	{"fat burning", regexp.MustCompile(`(?i)\b(burn(s|ing)? fat|fat[- ]burning|melt(s|ing)? fat)\b`)},
	{"fasting", regexp.MustCompile(`(?i)\b(fasting|fast for)\b`)},
	{"weight loss promise", regexp.MustCompile(`(?i)\blose \d+\s?(kg|kilos?|lbs?|pounds)\b`)},
	{"superfood", regexp.MustCompile(`(?i)\bsuperfoods?\b`)},
	{"emoji", regexp.MustCompile(`[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]`)},
}

const (
	minWords   = 80
	maxWords   = 240
	maxTitle   = 80
	listMarker = "\n1."
)

// Screen returns why a draft cannot be published, or nothing when it can.
func Screen(title, body string) []string {
	var problems []string
	text := title + "\n" + body
	for _, d := range denied {
		if d.re.MatchString(text) {
			problems = append(problems, d.reason)
		}
	}
	if t := strings.TrimSpace(title); t == "" || len([]rune(t)) > maxTitle {
		problems = append(problems, "title missing or too long")
	}
	switch n := len(strings.Fields(body)); {
	case n < minWords:
		problems = append(problems, "too short")
	case n > maxWords:
		problems = append(problems, "too long")
	}
	if strings.Contains("\n"+body, listMarker) {
		problems = append(problems, "numbered list")
	}
	return problems
}
