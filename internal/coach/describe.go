package coach

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// maxValueRunes caps one argument line. A long value is shortened, never
// dropped, so the person still sees that it is there.
const maxValueRunes = 240

// DescribeCall renders a call the way a person has to read it before allowing
// it: a title from the tool's name, then one "Label: value" line per argument,
// in the order the model wrote them.
//
// The arguments matter as much as the name: "log a check-in" is a very
// different request from "log a check-in saying the week went badly". They
// used to be shown as the raw JSON the model produced, which every client
// printed verbatim — web, iOS and Telegram alike. This is plain text with
// newlines, so each of them can show it as it is.
//
// Labels are derived from argument names and are English; values are the
// model's own words, usually already in the person's language.
func DescribeCall(call ai.ToolCall) string {
	title := humanize(call.Name)

	raw := bytes.TrimSpace(call.Arguments)
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return title
	}

	lines, ok := describeObject(raw)
	if !ok {
		return title + "\n" + shorten(string(raw))
	}
	if len(lines) == 0 {
		return title
	}
	return title + "\n" + strings.Join(lines, "\n")
}

// describeObject reads a JSON object's members in order. Decoding into a map
// would lose the order the model wrote them in, which is usually the order
// that reads best.
func describeObject(raw []byte) ([]string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}

	var lines []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, _ := keyTok.(string)

		var value any
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		text := inline(value)
		if text == "" {
			continue
		}
		lines = append(lines, humanize(key)+": "+shorten(text))
	}
	return lines, true
}

// inline renders one value on a single line. Nested objects become
// "key value, key value" and lists of them are separated by semicolons, so a
// meal plan's foods are readable without being reshaped per tool.
func inline(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.Join(strings.Fields(v), " ")
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "yes"
		}
		return "no"
	case []any:
		parts := make([]string, 0, len(v))
		sep := ", "
		for _, item := range v {
			if _, nested := item.(map[string]any); nested {
				sep = "; "
			}
			if s := inline(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, sep)
	case map[string]any:
		// Decoded into a map, a nested object has lost its member order, so
		// inlineObject imposes a reading order of its own.
		return inlineObject(v)
	default:
		return ""
	}
}

func inlineObject(v map[string]any) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sortKeysForReading(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if s := inline(v[k]); s != "" {
			parts = append(parts, strings.ToLower(humanize(k))+" "+s)
		}
	}
	return strings.Join(parts, ", ")
}

// sortKeysForReading puts identifying keys first, so "name Oats, grams 80"
// rather than "grams 80, name Oats", and the rest alphabetically for a stable
// result.
func sortKeysForReading(keys []string) {
	rank := func(k string) int {
		switch k {
		case "name", "title", "label":
			return 0
		case "source", "type", "kind":
			return 1
		}
		return 2
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if ri, rj := rank(keys[i]), rank(keys[j]); ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
}

// humanize turns create_workout_plan or daysPerWeek into "Create workout plan"
// and "Days per week".
func humanize(name string) string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i, r := range name {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.':
			flush()
		case unicode.IsUpper(r) && i > 0:
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	if len(words) == 0 {
		return name
	}
	s := strings.Join(words, " ")
	first, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(first)) + s[size:]
}

func shorten(s string) string {
	if utf8.RuneCountInString(s) <= maxValueRunes {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:maxValueRunes-1])) + "…"
}
