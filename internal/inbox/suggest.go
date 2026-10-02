package inbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/inbox/item"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// suggestAttempts is how many times one provider is asked before the walk
// moves on, for the reason capture gives: a malformed reply is not a refusal.
const suggestAttempts = 2

var suggestTemperature float32 = 0

// modelSuggestion is the reply shape. Goals are picked by their number in
// the prompt rather than by id, so the model cannot invent one.
type modelSuggestion struct {
	Destination string `json:"destination"`
	Goal        int    `json:"goal"`
	Title       string `json:"title"`
	Why         string `json:"why"`
}

func suggestSchema() *ai.Schema {
	return ai.Object("where this belongs", map[string]*ai.Schema{
		"destination": ai.Enum("the one home that fits", item.DestinationGoalNote, item.DestinationKnowledge, item.DestinationJournal),
		"goal":        ai.Integer("goal_note only: the goal's number in the list; 0 otherwise"),
		"title":       ai.String("knowledge only: a short title, at most eight words; empty otherwise"),
		"why":         ai.String("one short sentence for the person on why this home fits"),
	}, "destination", "goal", "title", "why")
}

// AISuggester is the production Suggester.
type AISuggester struct {
	runner *ai.Runner
	model  string
}

// NewAISuggester builds the suggester. model may be empty for the chain's
// default; a fast model is enough to sort one note.
func NewAISuggester(runner *ai.Runner, model string) *AISuggester {
	return &AISuggester{runner: runner, model: model}
}

func (a *AISuggester) Suggest(ctx context.Context, user users.User, text string, active []goals.Goal) (Suggestion, error) {
	numbered := make([]string, 0, len(active))
	for i, g := range active {
		numbered = append(numbered, fmt.Sprintf("%d. %s", i+1, g.Title))
	}
	system, err := prompts.Render(prompts.InboxTriage, map[string]any{"Goals": numbered})
	if err != nil {
		return Suggestion{}, apperr.Wrap(err, "render the inbox prompt")
	}
	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfaceInboxTriage)

	var reply modelSuggestion
	_, err = a.runner.Run(ctx, ai.RunOptions{Tier: string(user.Tier)}, func(client ai.Client) error {
		messages := []ai.Message{ai.UserText(text)}
		for attempt := 1; attempt <= suggestAttempts; attempt++ {
			resp, genErr := client.Generate(ctx, ai.Request{
				Model: a.model, System: system, Messages: messages,
				ResponseSchema: suggestSchema(), Temperature: &suggestTemperature,
			})
			if genErr != nil {
				return apperr.Wrap(genErr, "suggest an inbox home")
			}
			var candidate modelSuggestion
			decErr := json.Unmarshal(ai.JSONReply(resp.Text), &candidate)
			if decErr == nil {
				reply = candidate
				return nil
			}
			if attempt == suggestAttempts {
				return apperr.Wrap(decErr, "the reply was not valid JSON for the required shape")
			}
			messages = append(messages, ai.ModelText(resp.Text),
				ai.UserText("That was not valid JSON matching the schema. Answer again, correctly."))
		}
		return nil
	})
	if err != nil {
		return Suggestion{}, err
	}
	return resolve(reply, active), nil
}

// resolve turns the model's reply into a suggestion, falling back to the
// journal when it named a destination or goal that does not exist: a wrong
// guess the person corrects beats a goal note on the wrong goal.
func resolve(r modelSuggestion, active []goals.Goal) Suggestion {
	s := Suggestion{Destination: r.Destination, Why: r.Why}
	switch r.Destination {
	case item.DestinationGoalNote:
		if r.Goal < 1 || r.Goal > len(active) {
			return Suggestion{Destination: item.DestinationJournal, Why: r.Why}
		}
		g := active[r.Goal-1]
		s.GoalID, s.GoalTitle = &g.ID, g.Title
	case item.DestinationKnowledge:
		s.Title = r.Title
	case item.DestinationJournal:
	default:
		s.Destination = item.DestinationJournal
	}
	return s
}
