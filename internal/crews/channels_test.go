package crews_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestChannelsCRUD(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()

	ch, err := r.svc.CreateChannel(ctx, r.crew.ID, r.owner.ID, "  Leg Day! ", "Mondays")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Name != "leg-day" || ch.Topic != "Mondays" || ch.Kind != crew.ChannelText {
		t.Fatalf("created %+v", ch)
	}
	if _, err = r.svc.CreateChannel(ctx, r.crew.ID, r.ana.ID, "ana-only", ""); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a member creating a channel: %v", err)
	}
	if _, err = r.svc.CreateChannel(ctx, r.crew.ID, r.owner.ID, "leg_day", ""); fieldError(err, "name") == "" {
		t.Fatalf("a duplicate name: %v", err)
	}
	if _, err = r.svc.CreateChannel(ctx, r.crew.ID, r.owner.ID, "!!!", ""); fieldError(err, "name") == "" {
		t.Fatalf("an empty name: %v", err)
	}

	renamed, err := r.svc.UpdateChannel(ctx, r.crew.ID, ch.ID, r.owner.ID, "legs", "Every Monday")
	if err != nil || renamed.Name != "legs" || renamed.Topic != "Every Monday" {
		t.Fatalf("renamed %+v, %v", renamed, err)
	}
	if _, err = r.svc.UpdateChannel(ctx, r.crew.ID, r.general, r.owner.ID, "lobby", ""); fieldError(err, "name") == "" {
		t.Fatalf("renaming #general: %v", err)
	}
	if err = r.svc.DeleteChannel(ctx, r.crew.ID, r.general, r.owner.ID); fieldError(err, "channel") == "" {
		t.Fatalf("deleting #general: %v", err)
	}

	listed, err := r.svc.Channels(ctx, r.crew.ID, r.ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range listed {
		if c.Kind == crew.ChannelWins {
			t.Fatalf("#wins is listed before P2: %+v", listed)
		}
	}
	if len(listed) != 2 || listed[0].Name != "general" || listed[1].Name != "legs" {
		t.Fatalf("listed %+v", listed)
	}

	if err = r.svc.DeleteChannel(ctx, r.crew.ID, ch.ID, r.owner.ID); err != nil {
		t.Fatal(err)
	}
	// #general, #wins and 18 more make 20, the most a crew may have.
	for i := range crew.MaxChannels - 2 {
		if _, err = r.svc.CreateChannel(ctx, r.crew.ID, r.owner.ID, fmt.Sprintf("c%d", i), ""); err != nil {
			t.Fatalf("channel %d: %v", i, err)
		}
	}
	if _, err = r.svc.CreateChannel(ctx, r.crew.ID, r.owner.ID, "one-more", ""); fieldError(err, "name") == "" {
		t.Fatalf("channel %d: %v", crew.MaxChannels+1, err)
	}
}

func TestSetNotify(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	until := time.Now().Add(time.Hour)
	if err := r.svc.SetNotify(ctx, r.crew.ID, r.ana.ID, "all", &until); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.SetNotify(ctx, r.crew.ID, r.ana.ID, "loud", nil); fieldError(err, "notify") == "" {
		t.Fatalf("an unknown setting: %v", err)
	}
	var notify string
	if err := r.pool.QueryRow(ctx, `SELECT notify FROM crew_members WHERE crew_id = $1 AND user_id = $2`, r.crew.ID, r.ana.ID).Scan(&notify); err != nil || notify != "all" {
		t.Fatalf("notify = %q, %v", notify, err)
	}
}

func TestRemoveNotifiesRemoved(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, r.pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err = conn.Exec(ctx, "LISTEN "+live.Channel); err != nil {
		t.Fatal(err)
	}

	if err = r.svc.Remove(ctx, r.crew.ID, r.owner.ID, r.ana.ID); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n, err := conn.WaitForNotification(wait)
	if err != nil {
		t.Fatal(err)
	}
	var ev live.Event
	if err = json.Unmarshal([]byte(n.Payload), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Type != live.EventRemoved || ev.UserID != r.ana.ID || ev.CrewID != r.crew.ID {
		t.Fatalf("event = %+v", ev)
	}
}
