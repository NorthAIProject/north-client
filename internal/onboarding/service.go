// Package onboarding coordinates the first-run questionnaire: focus areas,
// coaching style, and one near-term goal. It owns no tables — it seeds data
// through users, memories, and goals.
package onboarding

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/memories"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Coaching style presets map to the free-text coaching_style column settings
// already uses.
const (
	StyleDirect     = "direct"
	StyleSupportive = "supportive"
	StyleSocratic   = "socratic"
	StyleCustom     = "custom"
)

// StyleText is the coaching_style value for each preset.
func StyleText(preset string) string {
	switch preset {
	case StyleDirect:
		return "Be direct. Skip pep talks. Challenge me."
	case StyleSupportive:
		return "Be warm and encouraging. Celebrate progress."
	case StyleSocratic:
		return "Ask questions. Help me think it through."
	default:
		return ""
	}
}

// FocusAreas are the life domains offered on the onboarding form (excluding
// "other", which is not a meaningful focus pick).
var FocusAreas = []string{
	lifedomain.Fitness,
	lifedomain.Health,
	lifedomain.Work,
	lifedomain.Learning,
	lifedomain.Personal,
}

// Answers is the validated output of the onboarding form.
type Answers struct {
	FocusAreas    []string
	CoachingStyle string
	NearTermGoal  string
	GoalCategory  string
}

// Coach opens the first conversation.
//
// An interface, and optional, for two reasons: onboarding must still work on a
// deployment with no AI provider configured, and taking *coach.Service directly
// would pull the whole context builder into this package's tests to seed three
// answers.
type Coach interface {
	StartConversation(ctx context.Context, userID uuid.UUID) (conversations.Conversation, error)
	SendMessage(ctx context.Context, user users.User, conversationID uuid.UUID, text string) (<-chan ai.StreamChunk, error)
}

type Service struct {
	users    *users.Service
	memories *memories.Service
	goals    *goals.Service

	// coach is nil when the first conversation should not be seeded. The
	// questionnaire still works; the person simply starts their own thread.
	coach Coach
	log   *slog.Logger
}

func NewService(u *users.Service, m *memories.Service, g *goals.Service) *Service {
	return &Service{users: u, memories: m, goals: g, log: slog.Default()}
}

// WithCoach seeds a first conversation from the answers. Follows
// documents.Service.WithEmbeddings: the feature is opt-in at wiring time, and
// absent it the rest behaves exactly as before.
func (s *Service) WithCoach(c Coach, log *slog.Logger) *Service {
	s.coach = c
	if log != nil {
		s.log = log
	}
	return s
}

// ValidateAnswers checks a submitted onboarding form.
func ValidateAnswers(focusAreas []string, stylePreset, styleCustom, goalTitle string) (Answers, error) {
	var errs apperr.FieldErrors

	areas := normalizeFocusAreas(focusAreas)
	if len(areas) == 0 {
		errs = errs.Add("focus_areas", "Pick at least one focus area.")
	}

	style := strings.TrimSpace(styleCustom)
	preset := strings.TrimSpace(stylePreset)
	switch preset {
	case StyleDirect, StyleSupportive, StyleSocratic:
		style = StyleText(preset)
	case StyleCustom:
		if style == "" {
			errs = errs.Add("coaching_style", "Describe how you want to be coached.")
		}
	default:
		errs = errs.Add("coaching_style", "Choose a coaching style.")
	}
	if len(style) > 1000 {
		errs = errs.Add("coaching_style", "Keep this under 1000 characters.")
	} else if len(style) > 0 && len(style) < 8 {
		errs = errs.Add("coaching_style", "Make it specific — a few more words help.")
	}

	title := strings.TrimSpace(goalTitle)
	if title == "" {
		errs = errs.Add("goal_title", "Name one goal you are working toward.")
	} else if len(title) > 200 {
		errs = errs.Add("goal_title", "Keep the name under 200 characters.")
	}

	if err := errs.OrNil(); err != nil {
		return Answers{}, err
	}

	return Answers{
		FocusAreas:    areas,
		CoachingStyle: style,
		NearTermGoal:  title,
		GoalCategory:  areas[0],
	}, nil
}

func normalizeFocusAreas(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, a := range raw {
		a = strings.TrimSpace(a)
		if !slices.Contains(FocusAreas, a) {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}

// Complete seeds coach context from the answers and marks onboarding finished.
// Idempotent once the user is already onboarded.
//
// The second return is the conversation opened from the answers, or uuid.Nil
// when none was. A cold start is what makes a coach feel like a chatbot: the
// first thread already having a question in it, answered with the goal and
// focus areas the person just gave, is most of the difference.
func (s *Service) Complete(ctx context.Context, user users.User, in Answers) (users.User, uuid.UUID, error) {
	if !user.NeedsOnboarding() {
		return user, uuid.Nil, nil
	}

	if _, err := s.users.UpdateProfile(ctx, user.ID, users.Profile{
		DisplayName:   user.DisplayName,
		Timezone:      user.Timezone,
		CoachingStyle: in.CoachingStyle,
	}); err != nil {
		return user, uuid.Nil, err
	}

	for _, area := range in.FocusAreas {
		content := "Focus area: " + area
		m, err := s.memories.Create(ctx, user.ID, memories.Input{
			Category: memories.CategoryPreference,
			Content:  content,
		})
		if err != nil {
			return user, uuid.Nil, err
		}
		if _, err := s.memories.SetPinned(ctx, m.ID, user.ID, true); err != nil {
			return user, uuid.Nil, err
		}
	}

	coachingMem, err := s.memories.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryCoaching,
		Content:  in.CoachingStyle,
	})
	if err != nil {
		return user, uuid.Nil, err
	}
	if _, err = s.memories.SetPinned(ctx, coachingMem.ID, user.ID, true); err != nil {
		return user, uuid.Nil, err
	}

	if _, err = s.goals.Create(ctx, user.ID, goals.Input{
		Title:    in.NearTermGoal,
		Category: in.GoalCategory,
	}); err != nil {
		return user, uuid.Nil, err
	}

	onboarded, err := s.users.MarkOnboarded(ctx, user.ID)
	if err != nil {
		return user, uuid.Nil, err
	}

	// Seeded last, and deliberately after MarkOnboarded: the context builder
	// reads the goal and memories written above, so the first reply is only
	// worth anything once they exist. A failure here is logged, not returned —
	// somebody who has answered three questions is onboarded whether or not a
	// provider was reachable in that moment.
	return onboarded, s.seedFirstConversation(ctx, onboarded, in), nil
}

// seedFirstConversation opens the thread and asks the coach the question the
// answers imply. Returns uuid.Nil when nothing was opened.
func (s *Service) seedFirstConversation(ctx context.Context, user users.User, in Answers) uuid.UUID {
	if s.coach == nil {
		return uuid.Nil
	}

	thread, err := s.coach.StartConversation(ctx, user.ID)
	if err != nil {
		s.log.Warn("could not open the first conversation", slog.Any("error", err),
			slog.String("user_id", user.ID.String()))
		return uuid.Nil
	}

	// SendMessage detaches generation from this request, so the redirect is not
	// held open while a model writes. The person lands on a thread already
	// being answered.
	if _, err = s.coach.SendMessage(ctx, user, thread.ID, openingMessage(in)); err != nil {
		s.log.Warn("could not seed the first conversation", slog.Any("error", err),
			slog.String("conversation_id", thread.ID.String()))
		return thread.ID
	}

	return thread.ID
}

// openingMessage is written in the person's own voice because it is stored as
// their message. Putting words in the coach's mouth that no model produced
// would make the transcript a lie.
func openingMessage(in Answers) string {
	var b strings.Builder
	b.WriteString("I am starting with North. My focus areas are ")
	b.WriteString(strings.Join(in.FocusAreas, ", "))
	b.WriteString(". The goal I am working toward is: ")
	b.WriteString(in.NearTermGoal)
	b.WriteString(". I can send a photo of where I am now, a clip of a lift, or just tell you what equipment and days I have. Where should I start, and what do you need to see?")
	return b.String()
}

// Skip marks onboarding finished without seeding data.
func (s *Service) Skip(ctx context.Context, user users.User) (users.User, error) {
	if !user.NeedsOnboarding() {
		return user, nil
	}
	return s.users.MarkOnboarded(ctx, user.ID)
}
