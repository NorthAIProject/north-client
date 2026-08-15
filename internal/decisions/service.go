package decisions

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const (
	// contextScan is how far back we look when ranking against a query.
	// A decision journal is small; fifty is more than anyone reviews in one
	// sitting and cheap enough to score in process.
	contextScan = 50
	// contextKeep is how many reach the coach. Same reasoning as mind's
	// contextEntries: a handful of relevant calls, not the archive.
	contextKeep = 5
	listDefault = 50
	listMax     = 100

	titleMax    = 200
	optionalMax = 2000
	minTokenLen = 3
)

// Input is a decision as submitted.
type Input struct {
	Title     string
	Options   string
	Rationale string
	Outcome   string
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.Title = strings.TrimSpace(in.Title)
	switch {
	case in.Title == "":
		errs = errs.Add("title", "Name the decision.")
	case len(in.Title) > titleMax:
		errs = errs.Add("title", "Keep this under 200 characters.")
	}

	in.Options = strings.TrimSpace(in.Options)
	if len(in.Options) > optionalMax {
		errs = errs.Add("options", "Keep this under 2000 characters.")
	}

	in.Rationale = strings.TrimSpace(in.Rationale)
	if len(in.Rationale) > optionalMax {
		errs = errs.Add("rationale", "Keep this under 2000 characters.")
	}

	in.Outcome = strings.TrimSpace(in.Outcome)
	if len(in.Outcome) > optionalMax {
		errs = errs.Add("outcome", "Keep this under 2000 characters.")
	}

	return in, errs.OrNil()
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (Decision, error) {
	clean, err := Validate(in)
	if err != nil {
		return Decision{}, err
	}
	return s.repo.Create(ctx, userID, clean.Title, clean.Options, clean.Rationale, clean.Outcome)
}

func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (Decision, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]Decision, error) {
	if limit <= 0 || limit > listMax {
		limit = listDefault
	}
	return s.repo.List(ctx, userID, limit)
}

func (s *Service) Update(ctx context.Context, id, userID uuid.UUID, in Input) (Decision, error) {
	clean, err := Validate(in)
	if err != nil {
		return Decision{}, err
	}
	return s.repo.Update(ctx, id, userID, clean.Title, clean.Options, clean.Rationale, clean.Outcome)
}

func (s *Service) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.Delete(ctx, id, userID)
}

// ForContext returns the decisions the coach should see this turn.
//
// With a query, entries are ranked by simple keyword overlap against title,
// options, rationale, and outcome. With no query — or no tokens worth matching
// — it falls back to the newest entries. Zero matches also fall back to newest:
// the coach should still know the person has been making calls, even if none
// of them mention today's words.
func (s *Service) ForContext(ctx context.Context, userID uuid.UUID, query string) ([]Decision, error) {
	list, err := s.repo.List(ctx, userID, contextScan)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}

	tokens := keywordTokens(query)
	if len(tokens) == 0 {
		return take(list, contextKeep), nil
	}

	type scored struct {
		d     Decision
		score int
	}
	var hits []scored
	for _, d := range list {
		if n := keywordScore(d, tokens); n > 0 {
			hits = append(hits, scored{d: d, score: n})
		}
	}
	if len(hits) == 0 {
		return take(list, contextKeep), nil
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].d.DecidedAt.After(hits[j].d.DecidedAt)
	})

	out := make([]Decision, 0, contextKeep)
	for i := 0; i < len(hits) && i < contextKeep; i++ {
		out = append(out, hits[i].d)
	}
	return out, nil
}

func keywordTokens(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	out := make([]string, 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		tok := strings.TrimFunc(f, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		if len(tok) < minTokenLen || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

func keywordScore(d Decision, tokens []string) int {
	hay := strings.ToLower(d.Title + " " + d.Options + " " + d.Rationale + " " + d.Outcome)
	n := 0
	for _, tok := range tokens {
		if strings.Contains(hay, tok) {
			n++
		}
	}
	return n
}

func take(list []Decision, n int) []Decision {
	if len(list) <= n {
		return list
	}
	return list[:n]
}
