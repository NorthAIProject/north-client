package exercises

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// defaultLimit bounds a browse page. Large enough that the common filters
// return everything they match, small enough that an unfiltered request does
// not render the whole catalog into one page.
const defaultLimit = 60

// maxLimit caps what a caller can ask for, including the plan generator's
// candidate list.
const maxLimit = 200

// candidateLimit is how many catalog rows the plan generator may put in front
// of the model.
//
// A cap rather than the whole catalog: the candidate list is prompt tokens on
// every generation, and a list long enough to include everything is long
// enough that the model stops reading it.
const candidateLimit = 80

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Exercise, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) GetBySlug(ctx context.Context, slug string) (Exercise, error) {
	return s.repo.GetBySlug(ctx, strings.TrimSpace(strings.ToLower(slug)))
}

// Search runs a browse query, returning the matches and the total count. The
// count is separate because the page shows "showing 60 of 143".
func (s *Service) Search(ctx context.Context, f Filter) ([]Exercise, int, error) {
	f = normalize(f)

	found, err := s.repo.Search(ctx, f)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return nil, 0, err
	}

	return found, total, nil
}

// Candidates returns exercises someone with this equipment could perform, for
// the plan generator to choose from.
//
// Bodyweight is always included regardless of what they own: an exercise that
// needs nothing is available to everyone, and leaving it out would mean
// someone with only dumbbells is never offered a push-up.
func (s *Service) Candidates(ctx context.Context, equipment []string) ([]Exercise, error) {
	allowed := []string{EquipmentNone}
	for _, item := range equipment {
		item = strings.TrimSpace(strings.ToLower(item))
		if item == "" || item == EquipmentNone {
			continue
		}
		allowed = append(allowed, item)
	}

	return s.repo.ForEquipment(ctx, allowed, candidateLimit)
}

// Resolve looks up the given slugs, returning only those the catalog knows.
//
// Missing slugs are not an error: the model may name an exercise the catalog
// does not carry, and that plan is still valid — it just keeps the model's own
// muscle keys instead of the catalog's.
func (s *Service) Resolve(ctx context.Context, slugs []string) (map[string]Exercise, error) {
	wanted := make([]string, 0, len(slugs))
	seen := make(map[string]bool, len(slugs))

	for _, slug := range slugs {
		slug = strings.TrimSpace(strings.ToLower(slug))
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		wanted = append(wanted, slug)
	}

	return s.repo.BySlugs(ctx, wanted)
}

// normalize applies the defaults and drops filter values outside the
// vocabularies, so a hand-typed query string cannot ask for a muscle that
// cannot exist and get a confusingly empty page.
func normalize(f Filter) Filter {
	f.Query = strings.TrimSpace(f.Query)

	f.Muscle = strings.TrimSpace(strings.ToLower(f.Muscle))
	if f.Muscle != "" && !plan.IsMuscleGroup(f.Muscle) {
		f.Muscle = ""
	}

	f.Category = strings.TrimSpace(strings.ToLower(f.Category))
	if f.Category != "" && !isCategory(f.Category) {
		f.Category = ""
	}

	equipment := make([]string, 0, len(f.Equipment))
	for _, item := range f.Equipment {
		if item = strings.TrimSpace(strings.ToLower(item)); item != "" {
			equipment = append(equipment, item)
		}
	}
	f.Equipment = equipment

	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}

	return f
}

func isCategory(value string) bool {
	for _, category := range Categories {
		if category == value {
			return true
		}
	}
	return false
}
