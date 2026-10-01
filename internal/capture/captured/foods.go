package captured

import "github.com/google/uuid"

// FoodDraft is a spoken or typed meal read into portions, waiting for a person
// to agree with it before any of it reaches a meal plan.
//
// It is a Draft narrowed to food, with the one thing a food log never needed:
// the other ingredients a name could have meant. A log takes one match or
// refuses; a meal being built is exactly where somebody wants to choose.
type FoodDraft struct {
	Lines []FoodLine `json:"lines"`

	// Unparsed is the text that became no portion — including anything the
	// parser read as water, sleep or a habit, which has no place in a meal.
	Unparsed []string `json:"unparsed"`
}

// FoodLine is one portion as heard.
type FoodLine struct {
	// Source is the person's own words for this line.
	Source string `json:"source"`
	// Query is the name that was looked up.
	Query string  `json:"query"`
	Grams float64 `json:"grams"`
	// Uncertain is set when the grams were converted or estimated — "two eggs"
	// rather than "120 grams of egg".
	Uncertain bool `json:"uncertain,omitempty"`

	// IngredientID is the match, when exactly one ingredient answers to the
	// name. Zero when the name was ambiguous and the person must choose.
	IngredientID uuid.UUID `json:"ingredient_id,omitzero"`
	MatchedName  string    `json:"matched_name,omitempty"`

	// Candidates is every ingredient the search offered, the match among them.
	Candidates []Candidate `json:"candidates"`

	// Problem is why this line cannot be added as it stands: nothing in the
	// catalog matched, or the catalog could not be searched.
	Problem string `json:"problem,omitempty"`
}

// Candidate is an ingredient a spoken name might mean.
type Candidate struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}
