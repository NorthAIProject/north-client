package vocab_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/habits/habit"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/voice/vocab"
)

type stubGoals struct {
	titles []string
	err    error
	calls  int
}

func (s *stubGoals) ListActive(_ context.Context, _ uuid.UUID) ([]goal.Goal, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]goal.Goal, 0, len(s.titles))
	for _, title := range s.titles {
		out = append(out, goal.Goal{Title: title})
	}
	return out, nil
}

type stubHabits struct {
	names []string
	err   error
	calls int
}

func (s *stubHabits) List(_ context.Context, _ users.User, _ bool) ([]habit.Habit, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]habit.Habit, 0, len(s.names))
	for _, name := range s.names {
		out = append(out, habit.Habit{Name: name})
	}
	return out, nil
}

func someone() users.User { return users.User{ID: uuid.New()} }

func TestTermsAreTheWordsThisPersonActuallyUses(t *testing.T) {
	goals := &stubGoals{titles: []string{"Run the Lisbon half marathon"}}
	habits := &stubHabits{names: []string{"Kettlebell swings", "Yerba mate"}}

	terms, err := vocab.New(goals, habits).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms: %v", err)
	}

	joined := strings.Join(terms, " | ")
	for _, want := range []string{"Run the Lisbon half marathon", "Kettlebell swings", "Yerba mate"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("terms are missing %q: %v", want, terms)
		}
	}
}

// Goals come first because the cap truncates from the end, and a goal title is
// the phrase somebody is most likely to say out loud.
func TestGoalsComeBeforeHabits(t *testing.T) {
	goals := &stubGoals{titles: []string{"Squat bodyweight"}}
	habits := &stubHabits{names: []string{"Morning pages"}}

	terms, err := vocab.New(goals, habits).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms: %v", err)
	}
	if len(terms) < 2 || terms[0] != "Squat bodyweight" {
		t.Fatalf("order = %v", terms)
	}
}

// A hint that fails is a hint that is missing, never a turn that fails: the
// person still gets their words transcribed, slightly less accurately.
func TestASourceThatFailsCostsAccuracyAndNothingElse(t *testing.T) {
	goals := &stubGoals{err: errors.New("the database is having a moment")}
	habits := &stubHabits{names: []string{"Kettlebell swings"}}

	terms, err := vocab.New(goals, habits).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms returned an error rather than degrading: %v", err)
	}
	if len(terms) != 1 || terms[0] != "Kettlebell swings" {
		t.Fatalf("terms = %v, want the source that worked", terms)
	}
}

func TestEverySourceFailingIsStillNotAnError(t *testing.T) {
	goals := &stubGoals{err: errors.New("nope")}
	habits := &stubHabits{err: errors.New("also nope")}

	terms, err := vocab.New(goals, habits).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms: %v", err)
	}
	if len(terms) != 0 {
		t.Fatalf("terms = %v, want none", terms)
	}
}

// A goal title can be a sentence. A sentence is not vocabulary, and spending the
// prompt window on one costs the terms that would have helped.
func TestSentencesAreNotVocabulary(t *testing.T) {
	long := "Get back to the version of myself that used to train four times a week before work"
	goals := &stubGoals{titles: []string{long, "Deadlift 140kg"}}

	terms, err := vocab.New(goals, &stubHabits{}).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms: %v", err)
	}
	for _, term := range terms {
		if term == long {
			t.Fatalf("a whole sentence was kept as vocabulary: %q", term)
		}
	}
	if len(terms) != 1 || terms[0] != "Deadlift 140kg" {
		t.Fatalf("terms = %v", terms)
	}
}

// The same word twice buys nothing and costs the window.
func TestTheSameTermIsOnlySentOnce(t *testing.T) {
	goals := &stubGoals{titles: []string{"Kettlebell swings"}}
	habits := &stubHabits{names: []string{"kettlebell swings", "Kettlebell Swings"}}

	terms, err := vocab.New(goals, habits).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("terms = %v, want one", terms)
	}
}

func TestEmptyAndBlankTermsAreDropped(t *testing.T) {
	goals := &stubGoals{titles: []string{"", "   ", "Swim 1km"}}

	terms, err := vocab.New(goals, &stubHabits{}).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms: %v", err)
	}
	if len(terms) != 1 || terms[0] != "Swim 1km" {
		t.Fatalf("terms = %v", terms)
	}
}

// Whisper's prompt window is small and truncates from the wrong end, so the cap
// belongs here rather than being discovered there.
func TestTheListIsCapped(t *testing.T) {
	var many []string
	for i := 0; i < 300; i++ {
		many = append(many, "Term number "+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	goals := &stubGoals{titles: many}

	terms, err := vocab.New(goals, &stubHabits{}).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms: %v", err)
	}
	if len(terms) > vocab.MaxTerms {
		t.Fatalf("kept %d terms, want at most %d", len(terms), vocab.MaxTerms)
	}
}

// Nil sources are a deployment that has not wired them, not a crash.
func TestNilSourcesAreNotAPanic(t *testing.T) {
	terms, err := vocab.New(nil, nil).VoiceTerms(context.Background(), someone())
	if err != nil {
		t.Fatalf("VoiceTerms: %v", err)
	}
	if len(terms) != 0 {
		t.Fatalf("terms = %v", terms)
	}
}
