package checkins_test

import (
	"context"
	"strings"
	"testing"

	"github.com/FACorreiaa/go-utils/pkg/util"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestValidateExtras(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in    checkins.Input
		field string
	}{
		"stress too low":     {checkins.Input{Mood: 3, Energy: 3, Stress: util.Ptr(0)}, "stress"},
		"stress too high":    {checkins.Input{Mood: 3, Energy: 3, Stress: util.Ptr(6)}, "stress"},
		"sleep too high":     {checkins.Input{Mood: 3, Energy: 3, SleepQuality: util.Ptr(9)}, "sleep_quality"},
		"too many tags":      {checkins.Input{Mood: 3, Energy: 3, Tags: strings.Split("a,b,c,d,e,f,g,h,i", ",")}, "tags"},
		"tag too long":       {checkins.Input{Mood: 3, Energy: 3, Tags: []string{strings.Repeat("x", 25)}}, "tags"},
		"multibyte too long": {checkins.Input{Mood: 3, Energy: 3, Tags: []string{strings.Repeat("é", 25)}}, "tags"},
	} {
		_, err := checkins.Validate(tc.in)
		var fe apperr.FieldErrors
		if !apperr.As(err, &fe) {
			t.Errorf("%s: want field errors, got %v", name, err)
			continue
		}
		if _, ok := fe.Messages()[tc.field]; !ok {
			t.Errorf("%s: want an error on %q, got %v", name, tc.field, fe.Messages())
		}
	}
}

func TestValidateNormalisesTagsAndDefaultsSource(t *testing.T) {
	t.Parallel()

	// Duplicates collapse before the limit is counted: nine typed, eight kept.
	tags := []string{" Travel", "travel", "sick", "a", "b", "c", "d", "e", strings.Repeat("é", 24)}
	clean, err := checkins.Validate(checkins.Input{Mood: 3, Energy: 3, Tags: tags, Stress: util.Ptr(5)})
	if err != nil {
		t.Fatal(err)
	}
	if len(clean.Tags) != 8 || clean.Tags[0] != "travel" {
		t.Fatalf("tags = %q, want 8 normalised tags starting with travel", clean.Tags)
	}
	if clean.Source != checkin.SourceUnknown {
		t.Fatalf("blank source = %q, want unknown", clean.Source)
	}
}

// The data-loss bug: the coach's create_check_in (mood and energy only) after
// a full morning check-in on the form replaced the whole row and wiped the
// wins and challenges.
func TestMergeTodayKeepsWhatTheWriterDidNotSay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "merge@north.test", "Europe/Lisbon")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	morning, err := svc.UpsertToday(ctx, user, checkins.Input{
		Mood: 3, Energy: 2, Wins: "ran 5k", Challenges: "slept badly", Notes: "deload week",
		Stress: util.Ptr(4), SleepQuality: util.Ptr(2), Tags: []string{"travel"},
		Source: checkin.SourceWeb,
	})
	if err != nil {
		t.Fatal(err)
	}

	merged, err := svc.MergeToday(ctx, user, checkins.Input{Mood: 5, Energy: 4, Source: checkin.SourceCoach})
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != morning.ID {
		t.Fatalf("merge made a second row: %s vs %s", merged.ID, morning.ID)
	}
	if merged.Mood != 5 || merged.Energy != 4 {
		t.Errorf("mood/energy not updated: %d/%d", merged.Mood, merged.Energy)
	}
	if merged.Wins != "ran 5k" || merged.Challenges != "slept badly" || merged.Notes != "deload week" {
		t.Errorf("text fields lost: %+v", merged)
	}
	if merged.Stress == nil || *merged.Stress != 4 || merged.SleepQuality == nil || *merged.SleepQuality != 2 {
		t.Errorf("stress/sleep lost: %v %v", merged.Stress, merged.SleepQuality)
	}
	if len(merged.Tags) != 1 || merged.Tags[0] != "travel" {
		t.Errorf("tags lost: %q", merged.Tags)
	}
	// Source records where the day's check-in was first created.
	if merged.Source != checkin.SourceWeb {
		t.Errorf("source = %q, want web (first creator)", merged.Source)
	}

	// A field the writer did give replaces the stored one.
	again, err := svc.MergeToday(ctx, user, checkins.Input{
		Mood: 4, Energy: 4, Notes: "feeling better", Stress: util.Ptr(1), Source: checkin.SourceCapture,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Notes != "feeling better" || *again.Stress != 1 || again.Wins != "ran 5k" {
		t.Errorf("given fields should replace, others stay: %+v", again)
	}
}

func TestMergeTodayCreatesWithItsSource(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "mergecreate@north.test", "Europe/Lisbon")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	c, err := svc.MergeToday(ctx, user, checkins.Input{Mood: 4, Energy: 3, Tags: []string{"Sick"}, Source: checkin.SourceMCP})
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != checkin.SourceMCP || len(c.Tags) != 1 || c.Tags[0] != "sick" {
		t.Fatalf("got source %q tags %q", c.Source, c.Tags)
	}
	if c.CreatedAt.IsZero() {
		t.Fatal("created_at not returned")
	}
}

// The form and the app's edits are full saves: a field left out is cleared,
// and the source stays where the check-in was first created.
func TestFullSavesClearExtrasAndKeepSource(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "fullsave@north.test", "Europe/Lisbon")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	created, err := svc.UpsertToday(ctx, user, checkins.Input{
		Mood: 3, Energy: 3, Wins: "walked", Stress: util.Ptr(3), Tags: []string{"work"}, Source: checkin.SourceSiri,
	})
	if err != nil {
		t.Fatal(err)
	}

	resaved, err := svc.UpsertToday(ctx, user, checkins.Input{Mood: 4, Energy: 4, Source: checkin.SourceWeb})
	if err != nil {
		t.Fatal(err)
	}
	if resaved.Stress != nil || len(resaved.Tags) != 0 || resaved.Wins != "" {
		t.Errorf("full save should clear omitted fields: %+v", resaved)
	}
	if resaved.Source != checkin.SourceSiri {
		t.Errorf("source = %q, want siri (first creator)", resaved.Source)
	}

	edited, err := svc.Update(ctx, created.ID, user.ID, checkins.Input{
		Mood: 2, Energy: 2, SleepQuality: util.Ptr(5), Source: checkin.SourceIOS,
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.SleepQuality == nil || *edited.SleepQuality != 5 || edited.Source != checkin.SourceSiri {
		t.Errorf("edit: sleep %v source %q", edited.SleepQuality, edited.Source)
	}
	if edited.Tags == nil {
		t.Error("tags must be non-nil for clients")
	}
}

func TestVersionMovesOnSaveEditAndDelete(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "version@north.test", "Europe/Lisbon")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	empty, err := svc.Version(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.UpsertToday(ctx, user, checkins.Input{Mood: 3, Energy: 3})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.Version(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved == empty {
		t.Fatal("version did not move on save")
	}
	if again, _ := svc.Version(ctx, user.ID); again != saved {
		t.Fatal("version moved with nothing saved")
	}
	if err = svc.Delete(ctx, c.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if deleted, _ := svc.Version(ctx, user.ID); deleted == saved {
		t.Fatal("version did not move on delete")
	}
}
