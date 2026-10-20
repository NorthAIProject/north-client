package crews

import (
	"context"
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
		MyRole:    crew.RoleOwner,
		Members: []Member{
			{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), DisplayName: "Ana", Handle: "ana_runs", CheckedIn: true, Streak: 3, WeekProgress: 3, Owner: true, Me: true, Role: crew.RoleOwner},
			{ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), DisplayName: "Leo", WorkedOut: true, Streak: 0, WeekProgress: 1, Role: crew.RoleAdmin},
		},
	}))
}

func TestCrewSpaceShape(t *testing.T) {
	t.Parallel()

	a := NewAPI(nil, "https://kheprios.com")
	until := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
	apitest.AssertGolden(t, "crew-space.golden.json", a.projectSpace(Space{
		Crew:       Crew{ID: uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"), Name: "Morning runners", Code: "q8w3n5k2ht", Members: 2},
		MyRole:     crew.RoleAdmin,
		Notify:     crew.NotifyMentions,
		MutedUntil: &until,
		Channels: []SpaceChannel{
			{Channel: Channel{ID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), Kind: crew.ChannelText, Name: "general", IsDefault: true}, Unread: 4, Mentions: 1},
			{Channel: Channel{ID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), Kind: crew.ChannelText, Name: "legs", Topic: "Mondays", Position: 2}},
		},
		Members: []SpaceMember{
			{Person: Person{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Name: "Ana", Handle: "ana_runs"}, Role: crew.RoleOwner},
			{Person: Person{ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), Name: "Leo"}, Role: crew.RoleAdmin},
		},
	}))
}

func TestCrewMessagesShape(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 20, 7, 30, 0, 0, time.UTC)
	general := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	ana := &Person{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Name: "Ana", Handle: "ana_runs"}
	apitest.AssertGolden(t, "crew-messages.golden.json", projectPage(context.Background(), Page{
		HasMore: true,
		Messages: []Message{
			{ID: uuid.MustParse("10000000-0000-0000-0000-000000000001"), Seq: 1, ChannelID: general, Kind: MessageKindSystem, Body: "crews.sys.welcome", CreatedAt: at},
			{
				ID: uuid.MustParse("10000000-0000-0000-0000-000000000002"), Seq: 2, ChannelID: general, Author: ana, Kind: MessageKindText,
				Body: "@leo 5k at 7?", ReplyTo: &Quote{ID: uuid.MustParse("10000000-0000-0000-0000-000000000009"), Unavailable: true},
				MentionsMe: true, Reactions: []Reaction{{Emoji: "fire", Count: 2, Mine: true}, {Emoji: "clap", Count: 1}},
				Pinned: true, Edited: true, CreatedAt: at.Add(time.Minute),
			},
			{
				ID: uuid.MustParse("10000000-0000-0000-0000-000000000003"), Seq: 3, ChannelID: general, Author: ana, Kind: MessageKindText,
				ReplyTo: &Quote{ID: uuid.MustParse("10000000-0000-0000-0000-000000000002"), AuthorName: "Ana", Excerpt: "@leo 5k at 7?"},
				Deleted: true, CreatedAt: at.Add(2 * time.Minute),
			},
		},
	}))
}
