// Package moderation keeps what people write to each other within the rules:
// a word filter that runs before anything is published, reports that hide
// content once enough people flag it, the terms a person accepts before
// their first post, and suspensions. Crews and the daily post both use it.
package moderation

import (
	"bufio"
	"embed"
	"errors"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ErrBlockedContent means text matched the filter and must not be published.
var ErrBlockedContent = errors.New("blocked content")

//go:embed wordlists/*.txt
var wordlists embed.FS

// Filter stops slurs, hate, and incitement to sexual violence or self-harm.
// It does not stop swearing: crews are gym talk. Every locale's list applies
// to every text, because a crew mixes languages.
type Filter struct {
	terms [][]string
}

// NewFilter loads the embedded lists. They ship in the binary, so a broken
// list is a build-time mistake and panics.
func NewFilter() *Filter {
	f := &Filter{}
	for _, list := range loadLists() {
		for _, term := range list {
			if words := words(term); len(words) > 0 {
				f.terms = append(f.terms, words)
			}
		}
	}
	return f
}

// Check returns ErrBlockedContent when text holds a listed term as whole
// words, ignoring case, accents and the usual digit-for-letter swaps.
func (f *Filter) Check(text string) error {
	have := words(text)
	for _, term := range f.terms {
		if containsRun(have, term) {
			return ErrBlockedContent
		}
	}
	return nil
}

func containsRun(have, term []string) bool {
	for i := 0; i+len(term) <= len(have); i++ {
		if slices.Equal(have[i:i+len(term)], term) {
			return true
		}
	}
	return false
}

// leet maps the stand-ins people type for letters.
var leet = map[rune]rune{'4': 'a', '@': 'a', '3': 'e', '1': 'i', '!': 'i', '0': 'o', '5': 's', '$': 's'}

// edgePunctuation is trimmed from both ends of a token before leet mapping,
// so "great!" stays "great" rather than becoming "greati".
const edgePunctuation = `.,;:!?"'()[]{}…`

// words lower-cases text, strips accents, undoes leetspeak inside tokens
// that are not plain numbers, and splits what is left into letter runs.
func words(text string) []string {
	var out []string
	for _, token := range strings.Fields(text) {
		token = strings.Trim(token, edgePunctuation)
		if token == "" {
			continue
		}
		var b strings.Builder
		numeric := isNumber(token)
		for _, r := range norm.NFKD.String(token) {
			switch {
			case unicode.Is(unicode.Mn, r):
				continue
			case unicode.IsLetter(r):
				b.WriteRune(unicode.ToLower(r))
			case !numeric && leet[r] != 0:
				b.WriteRune(leet[r])
			default:
				b.WriteRune(' ')
			}
		}
		out = append(out, strings.Fields(b.String())...)
	}
	return out
}

// isNumber is true for tokens like "100" or "2.5" that only hold digits and
// separators; their digits are digits, not letters in disguise.
func isNumber(token string) bool {
	for _, r := range token {
		if !unicode.IsDigit(r) && r != '.' && r != ',' {
			return false
		}
	}
	return true
}

// loadLists reads each embedded list, by file name without .txt. Lines
// starting with # are comments.
func loadLists() map[string][]string {
	entries, err := wordlists.ReadDir("wordlists")
	if err != nil {
		panic(err)
	}
	out := make(map[string][]string, len(entries))
	for _, e := range entries {
		file, err := wordlists.Open("wordlists/" + e.Name())
		if err != nil {
			panic(err)
		}
		scan := bufio.NewScanner(file)
		var terms []string
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				terms = append(terms, line)
			}
		}
		_ = file.Close()
		if err = scan.Err(); err != nil {
			panic(err)
		}
		out[strings.TrimSuffix(e.Name(), ".txt")] = terms
	}
	return out
}
