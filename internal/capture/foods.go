package capture

import (
	"context"
	"fmt"

	"github.com/NorthAIProject/north-client/internal/users"
)

// ParseFoods reads a meal out of a sentence and offers ingredients for each
// portion. Like Parse, it writes nothing.
//
// It asks the same parser with the same prompt as a capture. A meal is a
// capture that happens to be all food, and a second prompt that disagreed with
// the first about what "a handful of almonds" weighs would be worse than one
// prompt that is sometimes asked a narrower question.
func (s *Service) ParseFoods(ctx context.Context, user users.User, text string) (FoodDraft, error) {
	draft, err := s.parser.Parse(ctx, user, text, nil)
	if err != nil {
		return FoodDraft{}, err
	}

	out := FoodDraft{Lines: []FoodLine{}, Unparsed: draft.Unparsed}
	for _, item := range draft.Items {
		if item.Kind != KindFood || item.Food == nil {
			out.Unparsed = append(out.Unparsed, item.Source)
			continue
		}
		out.Lines = append(out.Lines, s.offer(ctx, user, item))
	}
	return out, nil
}

// offer searches the catalog for one portion and pre-selects the match when
// there is exactly one.
func (s *Service) offer(ctx context.Context, user users.User, item Item) FoodLine {
	line := FoodLine{
		Source:     item.Source,
		Query:      item.Food.Query,
		Grams:      item.Food.Grams,
		Uncertain:  item.Uncertain,
		Candidates: []Candidate{},
	}

	match, found, err := s.ingredients.Offer(ctx, user.ID, item.Food.Query, searchLimit)
	if err != nil {
		line.Problem = "The ingredient catalog could not be searched."
		return line
	}
	if len(found) == 0 {
		line.Problem = fmt.Sprintf("Nothing in the catalog matches %q.", item.Food.Query)
		return line
	}

	for _, ingredient := range found {
		line.Candidates = append(line.Candidates, Candidate{ID: ingredient.ID, Name: ingredient.Name})
	}
	if match != nil {
		line.IngredientID = match.ID
		line.MatchedName = match.Name
	}
	return line
}
