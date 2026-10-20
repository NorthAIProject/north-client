package crews_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/crews/crew"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social"
)

func TestEditOwnOnly(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	m := r.send(t, r.ana, "squat 100")

	if _, err := r.chat.Edit(ctx, r.crew.ID, m.ID, r.leo.ID, "squat 200"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("Leo editing Ana's message: %v", err)
	}
	if _, err := r.chat.Edit(ctx, r.crew.ID, m.ID, r.owner.ID, "squat 200"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("the owner editing Ana's message: %v", err)
	}
	if _, err := r.chat.Edit(ctx, r.crew.ID, m.ID, r.ana.ID, "  "); fieldError(err, "body") == "" {
		t.Fatalf("editing to nothing: %v", err)
	}
	got, err := r.chat.Edit(ctx, r.crew.ID, m.ID, r.ana.ID, "squat 105")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "squat 105" || !got.Edited {
		t.Fatalf("edited = %+v", got)
	}
}

func TestDeleteByAuthorOrManager(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	m := r.send(t, r.ana, "oops")
	if err := r.chat.React(ctx, r.crew.ID, m.ID, r.leo.ID, "fire", true); err != nil {
		t.Fatal(err)
	}

	if err := r.chat.Delete(ctx, r.crew.ID, m.ID, r.leo.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a member deleting another's message: %v", err)
	}
	if err := r.svc.SetRole(ctx, r.crew.ID, r.owner.ID, r.leo.ID, crew.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := r.chat.Delete(ctx, r.crew.ID, m.ID, r.leo.ID); err != nil {
		t.Fatalf("an admin deleting: %v", err)
	}
	got, err := r.chat.Get(ctx, r.crew.ID, m.ID, r.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Deleted || got.Body != "" || len(got.Reactions) != 0 || got.Author == nil {
		t.Fatalf("deleted = %+v", got)
	}

	own := r.send(t, r.ana, "mine")
	if err = r.chat.Delete(ctx, r.crew.ID, own.ID, r.ana.ID); err != nil {
		t.Fatalf("deleting your own: %v", err)
	}
}

func TestReactToggleAndBlockedNotCounted(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	m := r.send(t, r.owner, "PR day")
	for _, u := range []crews.Person{{ID: r.leo.ID}, {ID: r.ana.ID}} {
		if err := r.chat.React(ctx, r.crew.ID, m.ID, u.ID, "fire", true); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.chat.React(ctx, r.crew.ID, m.ID, r.ana.ID, "fire", true); err != nil {
		t.Fatalf("reacting twice: %v", err)
	}
	if err := r.chat.React(ctx, r.crew.ID, m.ID, r.ana.ID, "poop", true); fieldError(err, "emoji") == "" {
		t.Fatalf("unknown emoji: %v", err)
	}

	got, err := r.chat.Get(ctx, r.crew.ID, m.ID, r.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reactions) != 1 || got.Reactions[0] != (crews.Reaction{Emoji: "fire", Count: 2}) {
		t.Fatalf("owner sees %+v", got.Reactions)
	}

	if err = social.NewRepository(r.pool).Block(ctx, r.ana.ID, r.leo.ID); err != nil {
		t.Fatal(err)
	}
	got, err = r.chat.Get(ctx, r.crew.ID, m.ID, r.ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reactions) != 1 || got.Reactions[0] != (crews.Reaction{Emoji: "fire", Count: 1, Mine: true}) {
		t.Fatalf("Ana, who blocked Leo, sees %+v", got.Reactions)
	}

	if err = r.chat.React(ctx, r.crew.ID, m.ID, r.ana.ID, "fire", false); err != nil {
		t.Fatal(err)
	}
	if err = r.chat.React(ctx, r.crew.ID, m.ID, r.ana.ID, "fire", false); err != nil {
		t.Fatalf("un-reacting twice: %v", err)
	}
}

func TestPinManagersOnly(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	m := r.send(t, r.ana, "gym opens at 6")
	if err := r.chat.Pin(ctx, r.crew.ID, m.ID, r.ana.ID, true); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a member pinning: %v", err)
	}
	if err := r.chat.Pin(ctx, r.crew.ID, m.ID, r.owner.ID, true); err != nil {
		t.Fatal(err)
	}
	pins, err := r.chat.Pins(ctx, r.crew.ID, r.general, r.leo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || pins[0].ID != m.ID || !pins[0].Pinned {
		t.Fatalf("pins = %+v", pins)
	}
	if err = r.chat.Pin(ctx, r.crew.ID, m.ID, r.owner.ID, false); err != nil {
		t.Fatal(err)
	}
	if pins, err = r.chat.Pins(ctx, r.crew.ID, r.general, r.leo.ID); err != nil || len(pins) != 0 {
		t.Fatalf("after unpinning: %v, %v", pins, err)
	}
}

func TestUnreadAndMentions(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	r.send(t, r.leo, "first")
	second := r.send(t, r.leo, "@ana you in?")
	r.send(t, r.leo, "third")
	mine := r.send(t, r.ana, "yes")
	if _, err := r.chat.Send(ctx, r.crew.ID, r.general, r.leo.ID, crews.SendInput{Body: "nice", ReplyToID: &mine.ID}); err != nil {
		t.Fatal(err)
	}

	unread := func() crews.Unread {
		t.Helper()
		got, err := r.chat.Unread(ctx, r.ana.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range got {
			if u.ChannelID == r.general {
				return u
			}
		}
		return crews.Unread{}
	}
	if u := unread(); u.Count != 4 || u.Mentions != 2 || u.CrewID != r.crew.ID {
		t.Fatalf("unread = %+v; want 4 with 2 mentions", u)
	}
	if err := r.chat.MarkRead(ctx, r.crew.ID, r.general, r.ana.ID, second.Seq); err != nil {
		t.Fatal(err)
	}
	if u := unread(); u.Count != 2 || u.Mentions != 1 {
		t.Fatalf("after reading to the second = %+v; want 2 with 1 mention", u)
	}
	if err := r.chat.MarkRead(ctx, r.crew.ID, r.general, r.ana.ID, 1); err != nil {
		t.Fatal(err)
	}
	if u := unread(); u.Count != 2 {
		t.Fatalf("marking an older seq raised unread to %+v", u)
	}
}

func TestCreatePostsWelcome(t *testing.T) {
	r := newRoom(t)
	page, err := r.chat.List(context.Background(), r.crew.ID, r.general, r.ana.ID, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) == 0 || page.Messages[0].Kind != crews.MessageKindSystem || page.Messages[0].Body != "crews.sys.welcome" || page.Messages[0].Author != nil {
		t.Fatalf("first message = %+v", page.Messages)
	}
}
