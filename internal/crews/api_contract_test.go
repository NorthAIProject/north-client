package crews

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestCrewShapes(t *testing.T) {
	t.Parallel()

	a := NewAPI(nil, "https://kheprios.com")
	c := Crew{ID: uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"), Name: "Morning runners", Code: "q8w3n5k2ht", Members: 2}
	apitest.AssertGolden(t, "crews.golden.json", CrewList{Crews: []CrewView{a.projectCrew(c)}})
	apitest.AssertGolden(t, "crew.golden.json", a.projectCrew(c))
	apitest.AssertGolden(t, "crew-board.golden.json", a.projectBoard(Board{
		Crew:      c,
		Challenge: &Challenge{Kind: crew.ChallengeCheckIns, Target: 5},
		WeekStart: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
		IsOwner:   true,
		Members: []Member{
			{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), DisplayName: "Ana", Handle: "ana_runs", CheckedIn: true, Streak: 3, WeekProgress: 3, Owner: true, Me: true},
			{ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), DisplayName: "Leo", WorkedOut: true, Streak: 0, WeekProgress: 1},
		},
	}))
}
