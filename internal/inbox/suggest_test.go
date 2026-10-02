package inbox

import (
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/inbox/item"
)

func TestResolveNeverInventsAGoal(t *testing.T) {
	active := []goals.Goal{{ID: uuid.New(), Title: "Run a half marathon"}}
	if s := resolve(modelSuggestion{Destination: item.DestinationGoalNote, Goal: 1}, active); s.GoalID == nil || *s.GoalID != active[0].ID {
		t.Fatalf("goal 1 = %+v", s)
	}
	if s := resolve(modelSuggestion{Destination: item.DestinationGoalNote, Goal: 4}, active); s.Destination != item.DestinationJournal {
		t.Fatalf("goal out of range = %+v", s)
	}
	if s := resolve(modelSuggestion{Destination: "calendar"}, active); s.Destination != item.DestinationJournal {
		t.Fatalf("unknown destination = %+v", s)
	}
	if s := resolve(modelSuggestion{Destination: item.DestinationKnowledge, Title: "Zone 2"}, active); s.Title != "Zone 2" || s.GoalID != nil {
		t.Fatalf("knowledge = %+v", s)
	}
}
