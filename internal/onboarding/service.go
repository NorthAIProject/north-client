// Package onboarding coordinates the first-run questionnaire: focus areas,
// coaching style, and one near-term goal. It owns no tables — it seeds data
// through users, memories, and goals.
package onboarding

import (
	"context"
	"slices"
	"strings"

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

type Service struct {
	users    *users.Service
	memories *memories.Service
	goals    *goals.Service
}

func NewService(u *users.Service, m *memories.Service, g *goals.Service) *Service {
	return &Service{users: u, memories: m, goals: g}
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
func (s *Service) Complete(ctx context.Context, user users.User, in Answers) (users.User, error) {
	if !user.NeedsOnboarding() {
		return user, nil
	}

	if _, err := s.users.UpdateProfile(ctx, user.ID, users.Profile{
		DisplayName:   user.DisplayName,
		Timezone:      user.Timezone,
		CoachingStyle: in.CoachingStyle,
	}); err != nil {
		return user, err
	}

	for _, area := range in.FocusAreas {
		content := "Focus area: " + area
		m, err := s.memories.Create(ctx, user.ID, memories.Input{
			Category: memories.CategoryPreference,
			Content:  content,
		})
		if err != nil {
			return user, err
		}
		if _, err := s.memories.SetPinned(ctx, m.ID, user.ID, true); err != nil {
			return user, err
		}
	}

	coachingMem, err := s.memories.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryCoaching,
		Content:  in.CoachingStyle,
	})
	if err != nil {
		return user, err
	}
	if _, err := s.memories.SetPinned(ctx, coachingMem.ID, user.ID, true); err != nil {
		return user, err
	}

	if _, err := s.goals.Create(ctx, user.ID, goals.Input{
		Title:    in.NearTermGoal,
		Category: in.GoalCategory,
	}); err != nil {
		return user, err
	}

	return s.users.MarkOnboarded(ctx, user.ID)
}

// Skip marks onboarding finished without seeding data.
func (s *Service) Skip(ctx context.Context, user users.User) (users.User, error) {
	if !user.NeedsOnboarding() {
		return user, nil
	}
	return s.users.MarkOnboarded(ctx, user.ID)
}
