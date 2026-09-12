// Package vocab collects the words one person uses that a recogniser will
// otherwise mangle.
//
// Speech recognisers are good at English and bad at proper nouns. "Kettlebell"
// becomes "kettle bell", a habit somebody named "Yerba mate" comes back as
// "herb a mahtay", and a goal called "Deadlift 140kg" loses the number. The
// transcription endpoint takes a prompt that biases decoding, and the cheapest
// useful thing to put in it is the vocabulary this account already contains.
//
// It is its own package, below internal/voice rather than inside it, so that
// internal/voice keeps importing nothing but the AI layer. Everything that
// knows about goals and habits lives here, behind one method.
package vocab

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/habits/habit"
	"github.com/NorthAIProject/north-client/internal/users"
)

// MaxTerms bounds the list.
//
// Whisper's prompt window is 224 tokens, and an overrun is not an error — it is
// truncated, from the end that matters. Forty short phrases sit inside it with
// room to spare, and a person with more than forty active goals and habits has
// a problem this package cannot solve.
const MaxTerms = 40

// maxTermRunes drops anything too long to be a term.
//
// A goal title is free text and is often a sentence: "Get back to the version
// of myself that used to train four times a week". That is a paragraph of
// motivation, not a word a recogniser needs help with, and spending the prompt
// window on it costs the terms that would have helped.
const maxTermRunes = 40

// GoalLister is the slice of internal/goals this needs.
type GoalLister interface {
	ListActive(ctx context.Context, userID uuid.UUID) ([]goal.Goal, error)
}

// HabitLister is the slice of internal/habits this needs.
type HabitLister interface {
	List(ctx context.Context, user users.User, activeOnly bool) ([]habit.Habit, error)
}

// Source builds a vocabulary hint for one account.
type Source struct {
	goals  GoalLister
	habits HabitLister
	log    *slog.Logger
}

// New wires the sources. Either may be nil, which contributes nothing.
func New(goals GoalLister, habits HabitLister) *Source {
	return &Source{goals: goals, habits: habits, log: slog.Default()}
}

// WithLogger replaces the logger used to record a source that failed.
func (s *Source) WithLogger(log *slog.Logger) *Source {
	if log != nil {
		s.log = log
	}
	return s
}

// VoiceTerms returns the words worth biasing the recogniser toward.
//
// It never returns an error, and the signature keeps one only because the
// interface it satisfies has to allow for implementations that would. A hint is
// an accuracy improvement: losing it costs a slightly worse transcript, while
// failing the turn over it would cost the person their sentence. Every source
// that fails is logged and skipped.
//
// Goals come before habits, and the order is the truncation order — a goal
// title is the phrase somebody is most likely to say out loud.
func (s *Source) VoiceTerms(ctx context.Context, user users.User) ([]string, error) {
	var terms []string

	if s.goals != nil {
		active, err := s.goals.ListActive(ctx, user.ID)
		if err != nil {
			s.log.Warn("voice vocabulary: could not read goals", "error", err, "user_id", user.ID)
		}
		for _, g := range active {
			terms = append(terms, g.Title)
		}
	}

	if s.habits != nil {
		kept, err := s.habits.List(ctx, user, true)
		if err != nil {
			s.log.Warn("voice vocabulary: could not read habits", "error", err, "user_id", user.ID)
		}
		for _, h := range kept {
			terms = append(terms, h.Name)
		}
	}

	return clean(terms), nil
}

// clean drops what is not vocabulary and keeps each term once.
//
// Case-insensitive on the duplicate check but case-preserving in the output: a
// habit named "Yerba mate" and a goal mentioning "yerba mate" are one term, and
// the first spelling is the one sent, because the prompt is read as text.
func clean(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	out := make([]string, 0, min(len(terms), MaxTerms))

	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || len([]rune(term)) > maxTermRunes {
			continue
		}
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		out = append(out, term)
		if len(out) == MaxTerms {
			break
		}
	}
	return out
}
