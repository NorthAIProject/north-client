package news_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/news"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// fakeAggregator stands in for the feeds service.
type fakeAggregator struct {
	doc        news.Document
	candidates []news.Candidate
	itemsCalls [][]string
	discover   []string
}

func (f *fakeAggregator) Items(_ context.Context, feeds []string, _ int) (news.Document, error) {
	f.itemsCalls = append(f.itemsCalls, feeds)
	return f.doc, nil
}

func (f *fakeAggregator) Discover(_ context.Context, url string) ([]news.Candidate, error) {
	f.discover = append(f.discover, url)
	return f.candidates, nil
}

func newService(t *testing.T, agg *fakeAggregator, curated []string) (*news.Service, *preferences.Service, users.User) {
	t.Helper()
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: "fernando@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Fernando", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	prefs := preferences.NewService(preferences.NewRepository(pool))
	svc := news.NewService(news.NewRepository(pool), agg, prefs, curated)
	return svc, prefs, user
}

func item(key, title string, at time.Time) news.Item {
	return news.Item{Key: key, Title: title, URL: "https://pub.example/" + key, Source: "WHO", FeedURL: "https://who.int/rss", PublishedAt: at}
}

func TestSweepUpsertsIdempotentlyAndTickerReadsThem(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	agg := &fakeAggregator{doc: news.Document{Items: []news.Item{item("a", "A", now), item("b", "B", now.Add(-time.Hour))}}}
	svc, _, user := newService(t, agg, []string{"https://who.int/rss"})
	ctx := context.Background()

	if err := svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if agg.itemsCalls[0][0] != "https://who.int/rss" {
		t.Errorf("sweep should fetch the curated list: %v", agg.itemsCalls)
	}

	ticker, err := svc.Ticker(ctx, user, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !ticker.Enabled || len(ticker.Items) != 2 || ticker.Items[0].Key != "a" {
		t.Errorf("ticker = %+v", ticker)
	}
}

func TestTickerDisabledByPreference(t *testing.T) {
	agg := &fakeAggregator{doc: news.Document{Items: []news.Item{item("a", "A", time.Now())}}}
	svc, prefs, user := newService(t, agg, []string{"https://who.int/rss"})
	ctx := context.Background()
	_ = svc.Sweep(ctx)
	if err := prefs.SetNewsTickerEnabled(ctx, user.ID, false); err != nil {
		t.Fatal(err)
	}
	ticker, err := svc.Ticker(ctx, user, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ticker.Enabled || len(ticker.Items) != 0 {
		t.Errorf("ticker = %+v", ticker)
	}
}

func TestAddFeedDiscoversCapsAndDedupes(t *testing.T) {
	agg := &fakeAggregator{candidates: []news.Candidate{{URL: "https://site.example/feed", Type: "rss", Title: "Site"}}}
	svc, _, user := newService(t, agg, nil)
	svc.MaxUserFeeds = 2
	ctx := context.Background()

	added, err := svc.AddFeed(ctx, user, "https://site.example")
	if err != nil {
		t.Fatal(err)
	}
	if added.URL != "https://site.example/feed" || added.Title != "Site" {
		t.Errorf("added = %+v", added)
	}
	if _, dupErr := svc.AddFeed(ctx, user, "https://site.example"); !apperr.Is(dupErr, apperr.ErrValidation) {
		t.Errorf("duplicate should be a validation error, got %v", dupErr)
	}
	if _, schemeErr := svc.AddFeed(ctx, user, "ftp://nope"); !apperr.Is(schemeErr, apperr.ErrValidation) {
		t.Errorf("non-http should be a validation error, got %v", schemeErr)
	}
	agg.candidates = []news.Candidate{{URL: "https://two.example/feed", Type: "atom"}}
	if _, err = svc.AddFeed(ctx, user, "https://two.example"); err != nil {
		t.Fatal(err)
	}
	agg.candidates = []news.Candidate{{URL: "https://three.example/feed", Type: "atom"}}
	if _, capErr := svc.AddFeed(ctx, user, "https://three.example"); !apperr.Is(capErr, apperr.ErrValidation) {
		t.Errorf("over cap should be a validation error, got %v", capErr)
	}

	feeds, err := svc.Feeds(ctx, user)
	if err != nil || len(feeds) != 2 {
		t.Fatalf("feeds = %+v err=%v", feeds, err)
	}
	if err := svc.RemoveFeed(ctx, user, feeds[0].ID); err != nil {
		t.Fatal(err)
	}
	feeds, _ = svc.Feeds(ctx, user)
	if len(feeds) != 1 {
		t.Errorf("after remove = %d", len(feeds))
	}

	// The sweep now includes the user's feed alongside the curated list.
	agg.doc = news.Document{}
	_ = svc.Sweep(ctx)
	last := agg.itemsCalls[len(agg.itemsCalls)-1]
	if len(last) != 1 || last[0] != "https://two.example/feed" {
		t.Errorf("sweep feeds = %v", last)
	}
}
