package exercises

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

// matchExtraWords is how many words a catalog name may have beyond the ones
// asked for and still be "the same exercise": "Bench Press (Barbell)" is
// "Barbell Bench Press - Medium Grip" with two to spare. More than that and
// the catalog entry is a different movement that happens to share words.
const matchExtraWords = 2

// Matcher finds the catalog exercise another app's exercise name means. It
// is built once from the whole catalog and is safe to reuse across names.
type Matcher struct {
	slugs   map[string]bool
	byWords map[string][]string // sorted words → slugs with exactly those words
	entries []matchEntry
}

type matchEntry struct {
	slug  string
	words map[string]bool
}

// NewMatcher indexes catalog for Match.
func NewMatcher(catalog []Exercise) *Matcher {
	m := &Matcher{slugs: map[string]bool{}, byWords: map[string][]string{}}
	for _, e := range catalog {
		m.slugs[e.Slug] = true
		words := nameWords(e.Name)
		if len(words) == 0 {
			continue
		}
		key := wordsKey(words)
		m.byWords[key] = append(m.byWords[key], e.Slug)
		m.entries = append(m.entries, matchEntry{slug: e.Slug, words: words})
	}
	return m
}

// Match returns the catalog slug name refers to, trying in order: a slug
// written as-is, the alias table for other apps' names, a catalog name with
// exactly the same words, and finally the one catalog name that contains all
// of name's words with the fewest extra (at most matchExtraWords). Anything
// ambiguous is no match: a free-text exercise is honest, a wrong catalog
// entry lights the wrong muscles.
func (m *Matcher) Match(name string) (string, bool) {
	if slug := strings.ToLower(strings.TrimSpace(name)); m.slugs[slug] {
		return slug, true
	}
	words := nameWords(name)
	if len(words) == 0 {
		return "", false
	}
	key := wordsKey(words)
	if slug, ok := aliases[key]; ok && m.slugs[slug] {
		return slug, true
	}
	if slugs := m.byWords[key]; len(slugs) == 1 {
		return slugs[0], true
	}

	best, bestExtra, tied := "", matchExtraWords+1, false
	for _, e := range m.entries {
		if !containsAll(e.words, words) {
			continue
		}
		extra := len(e.words) - len(words)
		switch {
		case extra < bestExtra:
			best, bestExtra, tied = e.slug, extra, false
		case extra == bestExtra:
			tied = true
		}
	}
	if best == "" || tied {
		return "", false
	}
	return best, true
}

// Matcher loads the whole catalog into a Matcher.
func (s *Service) Matcher(ctx context.Context) (*Matcher, error) {
	// Equipment must be an empty slice, not nil: the query reads a NULL array
	// as "no row matches", where an empty one means "no constraint".
	all, err := s.repo.Search(ctx, Filter{Limit: wholeCatalog, Equipment: []string{}})
	if err != nil {
		return nil, err
	}
	return NewMatcher(all), nil
}

// wholeCatalog bounds Matcher's read; the catalog is a few hundred rows.
const wholeCatalog = 5000

// nameWords reduces an exercise name to the set of words that identify it:
// lower case, punctuation and brackets dropped, common abbreviations and
// plurals folded, filler words removed. "Bench Press (Barbell)" and
// "barbell bench-press" give the same set.
func nameWords(name string) map[string]bool {
	fields := strings.FieldsFunc(compounds.Replace(strings.ToLower(name)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	words := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f = foldWord(f); f != "" {
			words[f] = true
		}
	}
	return words
}

func foldWord(w string) string {
	if fillerWords[w] {
		return ""
	}
	if canonical, ok := wordForms[w]; ok {
		return canonical
	}
	return w
}

// compounds splits words some apps write closed: "pullup" is "pull up".
var compounds = strings.NewReplacer("pullups", "pull ups", "pullup", "pull up", "pushups", "push ups", "pushup", "push up",
	"chinups", "chin ups", "chinup", "chin up")

var fillerWords = map[string]bool{"a": true, "an": true, "the": true, "with": true, "and": true, "on": true, "of": true, "to": true}

// wordForms folds abbreviations and plurals onto one spelling.
var wordForms = map[string]string{
	"db": "dumbbell", "dumbbells": "dumbbell", "bb": "barbell", "barbells": "barbell", "kb": "kettlebell",
	"kettlebells": "kettlebell", "ups": "up",
	"curls": "curl", "raises": "raise", "rows": "row", "extensions": "extension", "flyes": "fly", "flys": "fly",
	"flye": "fly", "flies": "fly", "crunches": "crunch", "lunges": "lunge", "squats": "squat", "deadlifts": "deadlift",
	"dips": "dip", "presses": "press", "shrugs": "shrug", "pulldowns": "pulldown", "pushdowns": "pushdown",
	"thrusts": "thrust", "triceps": "tricep", "biceps": "bicep", "skullcrushers": "skullcrusher",
}

func wordsKey(words map[string]bool) string {
	list := make([]string, 0, len(words))
	for w := range words {
		list = append(list, w)
	}
	sort.Strings(list)
	return strings.Join(list, " ")
}

func containsAll(have, want map[string]bool) bool {
	for w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}
