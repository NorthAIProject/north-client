package search

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/shared/limits"
)

// ErrEmptyTerm reports a query with nothing to match on.
//
// Its own error because callers treat it as a normal outcome rather than a
// failure: the coach's first turn in a new conversation has no query, and the
// right response is to contribute no search results, not to log an error.
var ErrEmptyTerm = fmt.Errorf("search term is empty")

// Normalise prepares user text for use as a tsquery argument.
//
// It does not build a query expression. Every caller passes the result to
// websearch_to_tsquery, which is the only tsquery function that accepts
// arbitrary human input: to_tsquery raises a syntax error on a stray quote or
// ampersand, and plainto_tsquery throws away the phrase and negation syntax
// people actually type. websearch_to_tsquery treats operators it does not
// understand as ordinary words, so no input can change the shape of the query
// or reach the parser as anything but a value.
//
// That makes this function's job narrow on purpose: reject what is not worth a
// round trip, and bound what is.
func Normalise(term string) (string, error) {
	trimmed := strings.TrimSpace(term)
	if trimmed == "" {
		return "", ErrEmptyTerm
	}

	if utf8.RuneCountInString(trimmed) > limits.MaxSearchTermLength {
		return "", fmt.Errorf("search term must be at most %d characters, got %d",
			limits.MaxSearchTermLength, utf8.RuneCountInString(trimmed))
	}

	// Collapse whitespace. A pasted paragraph carries newlines and runs of
	// spaces that mean nothing to the parser but do count against the length
	// bound above and make the term unreadable in a log.
	return strings.Join(strings.Fields(trimmed), " "), nil
}
