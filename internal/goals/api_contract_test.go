package goals

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestGoalShapes(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	progress := 40
	update := GoalUpdateView{ID: uuid.MustParse("a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1"), Note: "Ran 12 km, felt easy.", Progress: &progress, CreatedAt: created.AddDate(0, 0, 10)}
	summary := GoalSummary{
		ID: uuid.MustParse("b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2"), Title: "Run a half marathon", Category: "fitness",
		Status: "active", TargetDate: "2026-12-06", MilestoneTotal: 2, MilestoneDone: 1, LatestUpdate: &update, CreatedAt: created,
	}
	apitest.AssertGolden(t, "goals.golden.json", GoalList{Goals: []GoalSummary{summary}, Categories: []string{"fitness", "health", "work", "learning", "personal", "other"}})

	done := created.AddDate(0, 0, 14)
	apitest.AssertGolden(t, "goal.golden.json", GoalDetail{
		GoalSummary: summary, Motivation: "Prove I can keep a plan for three months.", Success: "Finish under 2:10.",
		Milestones: []MilestoneView{
			{ID: uuid.MustParse("c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3"), Title: "Run 10 km", Status: "completed", CompletedAt: &done},
			{ID: uuid.MustParse("d4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4"), Title: "Run 16 km", Status: "open", TargetDate: "2026-11-01"},
		},
		Updates: []GoalUpdateView{update},
	})
}
