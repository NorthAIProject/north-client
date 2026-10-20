package crews

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

var (
	spaceCrew    = uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	spaceGeneral = uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	spaceAna     = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	spaceLeo     = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

func spaceFixture() SpaceView {
	general := crew.SpaceChannel{Channel: crew.Channel{ID: spaceGeneral, Kind: crew.ChannelText, Name: "general", IsDefault: true}}
	at := time.Date(2026, 10, 20, 7, 30, 0, 0, time.UTC)
	leo := &crew.Person{ID: spaceLeo, Name: "Leo", Handle: "leo"}
	return SpaceView{
		Space: crew.Space{
			Crew:   crew.Crew{ID: spaceCrew, Name: "Morning crew", Members: 2},
			MyRole: crew.RoleOwner,
			Notify: crew.NotifyMentions,
			Channels: []crew.SpaceChannel{
				general,
				{Channel: crew.Channel{ID: uuid.New(), Kind: crew.ChannelText, Name: "legs", Topic: "Mondays"}, Unread: 2, Mentions: 1},
			},
			Members: []crew.SpaceMember{
				{Person: crew.Person{ID: spaceAna, Name: "Ana", Handle: "ana"}, Role: crew.RoleOwner},
				{Person: crew.Person{ID: spaceLeo, Name: "Leo", Handle: "leo"}, Role: crew.RoleMember},
			},
		},
		Mine:    []crew.Crew{{ID: spaceCrew, Name: "Morning crew"}},
		Channel: general,
		Tab:     TabChat,
		Me:      spaceAna,
		JoinURL: "https://khepri.test/i/c/x",
		Page: crew.Page{Messages: []crew.Message{
			{ID: uuid.New(), Seq: 1, ChannelID: spaceGeneral, Kind: crew.MessageKindSystem, Body: "crews.sys.welcome", CreatedAt: at},
			{
				ID: uuid.New(), Seq: 2, ChannelID: spaceGeneral, Kind: crew.MessageKindText, Author: leo, Body: "<script>alert(1)</script> @ana in?", MentionsMe: true, CreatedAt: at.Add(time.Minute),
				Reactions: []crew.Reaction{{Emoji: "fire", Count: 2, Mine: true}},
			},
			{ID: uuid.New(), Seq: 3, ChannelID: spaceGeneral, Kind: crew.MessageKindText, Author: leo, Body: "6am", CreatedAt: at.Add(2 * time.Minute)},
		}},
	}
}

func renderSpaceIn(t *testing.T, locale users.Locale, v SpaceView) string {
	t.Helper()
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	if err := SpacePage(users.User{ID: spaceAna, DisplayName: "Ana", Locale: locale}, v).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return b.String()
}

func TestSpaceRendersChannelsAndMessages(t *testing.T) {
	html := renderSpaceIn(t, users.LocaleEN, spaceFixture())
	for _, want := range []string{
		"general", "legs",
		`hx-sse:connect="/app/crews/` + spaceCrew.String() + `/live?channel=` + spaceGeneral.String() + `"`,
		`id="crew-cursor" name="after" value="3"`,
		"Welcome to #general. Say hi",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"🔥",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page does not contain %q", want)
		}
	}
	for _, leak := range []string{"crews.sys.welcome", "<script>alert(1)"} {
		if strings.Contains(html, leak) {
			t.Errorf("page contains %q", leak)
		}
	}
}

func TestSpaceChallengeTabShowsTheBoard(t *testing.T) {
	v := spaceFixture()
	v.Tab = TabChallenge
	v.Board = crew.Board{
		Crew:    v.Space.Crew,
		Members: []crew.Member{{ID: spaceAna, DisplayName: "Ana", Me: true, Owner: true, Streak: 4, Role: crew.RoleOwner}},
		IsOwner: true, MyRole: crew.RoleOwner,
	}
	html := renderSpaceIn(t, users.LocaleEN, v)
	if !strings.Contains(html, "4-day check-in streak") || strings.Contains(html, `id="crew-cursor"`) {
		t.Fatal("the challenge tab should show the board and no transcript")
	}
}

func TestSpaceRendersInTheChosenLanguage(t *testing.T) {
	html := renderSpaceIn(t, users.LocaleES, spaceFixture())
	for _, want := range []string{"Te damos la bienvenida a #general", "Escribe en #general", "Reto"} {
		if !strings.Contains(html, want) {
			t.Errorf("es: page does not contain %q", want)
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedSpace(t *testing.T) {
	englishOnly := []string{"Welcome to #general", "Message #general", "Challenge", "Pinned messages", "Notifications", "Invite link", "Leave crew", "New channel", "Report message", "Crew rules"}
	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		html := renderSpaceIn(t, locale, spaceFixture())
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}
