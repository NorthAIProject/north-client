package lighterday_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/lighterday"
)

type grants struct{ ids []string }

func (g *grants) Grant(_ context.Context, _ uuid.UUID, _ time.Time, id string) {
	g.ids = append(g.ids, id)
}

func TestCoachLighterDayIsNotListening(t *testing.T) {
	ctx := context.Background()
	u, build := user(t)
	g := &grants{}
	svc := build(readings{hrvToday: 38, rhrToday: 56}, session{due: true}).WithAwards(g)

	if _, err := svc.ChooseAs(ctx, u, lighterday.ChoiceLighter, lighterday.SourceCoach); err != nil {
		t.Fatal(err)
	}
	if len(g.ids) != 0 {
		t.Fatalf("coach auto-lighter granted %v", g.ids)
	}
	if _, err := svc.ChooseAs(ctx, u, lighterday.ChoiceLighter, lighterday.SourcePerson); err != nil {
		t.Fatal(err)
	}
	if len(g.ids) != 1 || g.ids[0] != "listened_to_body" {
		t.Fatalf("person lighter on a low morning granted %v", g.ids)
	}
}

func TestLighterOnANormalMorningIsNotListening(t *testing.T) {
	ctx := context.Background()
	u, build := user(t)
	g := &grants{}
	svc := build(readings{hrvToday: 52, rhrToday: 56}, session{due: true}).WithAwards(g)
	if _, err := svc.Choose(ctx, u, lighterday.ChoiceLighter); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Choose(ctx, u, lighterday.ChoiceKeep); err != nil {
		t.Fatal(err)
	}
	if len(g.ids) != 0 {
		t.Fatalf("granted %v", g.ids)
	}
}
