// Package inbox keeps what a person saved without deciding where it belongs:
// from the share sheet, a shortcut, or a quick note in the app. The coach
// suggests a home in the background; the person confirms or picks another.
// Nothing is ever filed without them.
package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/inbox/item"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/mind"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

type (
	Item       = item.Item
	Suggestion = item.Suggestion
	Filing     = item.Filing
)

// listLimit is how many open items one list returns. An inbox with more than
// this open is a backlog the person should be nudged to clear, not paged.
const listLimit = 100

// Enqueuer schedules the background suggestion. *jobs.Queue satisfies it.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind jobs.Kind, payload any) (jobs.Job, error)
}

// Suggester proposes where an item belongs, given the person's active goals.
type Suggester interface {
	Suggest(ctx context.Context, user users.User, text string, active []goals.Goal) (Suggestion, error)
}

// Goals, Notes and Journal are the slices an item can be filed into.
type Goals interface {
	ListActive(ctx context.Context, userID uuid.UUID) ([]goals.Goal, error)
	AddUpdate(ctx context.Context, goalID, userID uuid.UUID, note string, progress *int) (goals.Update, error)
}

type Notes interface {
	CreateNote(ctx context.Context, userID uuid.UUID, title, body string) (documents.Document, error)
}

type Journal interface {
	Create(ctx context.Context, userID uuid.UUID, in mind.Input) (mind.JournalEntry, error)
}

// Users loads the account a job is for.
type Users interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
}

type Service struct {
	repo      Repository
	queue     Enqueuer
	suggester Suggester
	goals     Goals
	notes     Notes
	journal   Journal
	users     Users
	log       *slog.Logger
}

type Options struct {
	Queue     Enqueuer
	Suggester Suggester
	Goals     Goals
	Notes     Notes
	Journal   Journal
	Users     Users
	Log       *slog.Logger
}

// Repository is the persistence Service depends on: the items people saved
// and what became of them. NewRepository is what the process wires, and a
// test can hand Service anything that keeps items.
type Repository interface {
	Add(ctx context.Context, userID uuid.UUID, source, text string) (Item, error)
	ListOpen(ctx context.Context, userID uuid.UUID, limit int) ([]Item, error)
	CountOpen(ctx context.Context, userID uuid.UUID) (int, error)
	Get(ctx context.Context, itemID, userID uuid.UUID) (Item, error)
	SetSuggestion(ctx context.Context, itemID, userID uuid.UUID, sug Suggestion) error
	MarkFiled(ctx context.Context, itemID, userID uuid.UUID, destination string, ref uuid.UUID) error
	Dismiss(ctx context.Context, itemID, userID uuid.UUID) error
}

func NewService(repo Repository, o Options) *Service {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		repo: repo, queue: o.Queue, suggester: o.Suggester,
		goals: o.Goals, notes: o.Notes, journal: o.Journal, users: o.Users, log: log,
	}
}

// Add saves an item and asks for a suggestion in the background, so saving
// from the share sheet is instant whatever the model is doing.
func (s *Service) Add(ctx context.Context, userID uuid.UUID, source, text string) (Item, error) {
	text = strings.TrimSpace(text)
	var errs apperr.FieldErrors
	switch {
	case text == "":
		errs = errs.Add("text", "Write or share something to save.")
	case len([]rune(text)) > item.MaxText:
		errs = errs.Add("text", fmt.Sprintf("Keep it under %d characters; documents go to Knowledge.", item.MaxText))
	}
	if !item.ValidSource(source) {
		errs = errs.Add("source", "Unknown source.")
	}
	if err := errs.OrNil(); err != nil {
		return Item{}, err
	}
	it, err := s.repo.Add(ctx, userID, source, text)
	if err != nil {
		return Item{}, err
	}
	if s.queue != nil {
		// The item is saved either way; a missing suggestion only means the
		// person picks the home themselves.
		if _, err := s.queue.Enqueue(ctx, jobs.KindSuggestInbox, jobs.SuggestInboxPayload{UserID: userID, ItemID: it.ID}); err != nil {
			s.log.Warn("could not queue inbox suggestion", slog.Any("error", err))
		}
	}
	return it, nil
}

// Open lists what is still waiting, newest first, and how many there are.
func (s *Service) Open(ctx context.Context, userID uuid.UUID) ([]Item, int, error) {
	out, err := s.repo.ListOpen(ctx, userID, listLimit)
	if err != nil {
		return nil, 0, err
	}
	count, err := s.repo.CountOpen(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	return out, count, nil
}

// HandleSuggestJob is the worker's handler for jobs.KindSuggestInbox.
func (s *Service) HandleSuggestJob(ctx context.Context, payload json.RawMessage) error {
	var p jobs.SuggestInboxPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode inbox suggestion job")
	}
	return s.Suggest(ctx, p.UserID, p.ItemID)
}

// Suggest asks the coach where one item belongs and stores the answer. An
// item already filed or dismissed is left alone.
func (s *Service) Suggest(ctx context.Context, userID, itemID uuid.UUID) error {
	if s.suggester == nil {
		return nil
	}
	saved, err := s.repo.Get(ctx, itemID, userID)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if saved.Status != item.StatusOpen {
		return nil
	}
	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return err
	}
	active, err := s.goals.ListActive(ctx, userID)
	if err != nil {
		return err
	}
	sug, err := s.suggester.Suggest(ctx, user, saved.Text, active)
	if err != nil {
		return err
	}
	return s.repo.SetSuggestion(ctx, itemID, userID, sug)
}

// File writes an item where the person chose and marks it filed.
func (s *Service) File(ctx context.Context, userID, itemID uuid.UUID, f Filing) (Item, error) {
	if !item.ValidDestination(f.Destination) {
		return Item{}, apperr.FieldErrors{}.Add("destination", "Choose a goal, Knowledge or the journal.")
	}
	saved, err := s.repo.Get(ctx, itemID, userID)
	if err != nil {
		return Item{}, err
	}
	if saved.Status != item.StatusOpen {
		return Item{}, apperr.FieldErrors{}.Add("item", "That was already filed or dismissed.")
	}

	var ref uuid.UUID
	switch f.Destination {
	case item.DestinationGoalNote:
		if f.GoalID == uuid.Nil {
			return Item{}, apperr.FieldErrors{}.Add("goal", "Choose the goal it is about.")
		}
		u, fileErr := s.goals.AddUpdate(ctx, f.GoalID, userID, saved.Text, nil)
		if fileErr != nil {
			return Item{}, fileErr
		}
		ref = u.ID
	case item.DestinationKnowledge:
		doc, fileErr := s.notes.CreateNote(ctx, userID, noteTitle(f.Title, saved.Text), saved.Text)
		if fileErr != nil {
			return Item{}, fileErr
		}
		ref = doc.ID
	case item.DestinationJournal:
		e, fileErr := s.journal.Create(ctx, userID, mind.Input{Content: saved.Text})
		if fileErr != nil {
			return Item{}, fileErr
		}
		ref = e.ID
	}

	if err = s.repo.MarkFiled(ctx, itemID, userID, f.Destination, ref); err != nil {
		return Item{}, err
	}
	saved.Status = item.StatusFiled
	return saved, nil
}

// Dismiss drops an item without filing it.
func (s *Service) Dismiss(ctx context.Context, userID, itemID uuid.UUID) error {
	return s.repo.Dismiss(ctx, itemID, userID)
}

// noteTitle is the chosen title, or the first line of the text cut to a
// heading's length.
func noteTitle(title, text string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if r := []rune(first); len(r) > 60 {
		return strings.TrimSpace(string(r[:60])) + "…"
	}
	return first
}
