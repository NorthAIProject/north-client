package news_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/news"
)

const jsonFeedBody = `{"version":"https://jsonfeed.org/version/1.1","title":"Merged timeline","items":[
 {"id":"a","url":"https://who.int/a","title":"WHO update","date_published":"2026-09-17T11:00:00Z","_feeds":{"source_name":"WHO","source_url":"https://who.int","feed_url":"https://who.int/rss"}},
 {"id":"b","title":"No link is dropped","date_published":"2026-09-17T10:00:00Z","_feeds":{"source_name":"NIH","feed_url":"https://nih.gov/rss"}}],
 "_feeds":{"warming":["https://nih.gov/rss"],"stale":[],"generated_at":"2026-09-17T12:00:00Z"}}`

func TestClientItemsParsesJSONFeed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/items" || r.URL.Query().Get("limit") != "50" || r.URL.Query().Get("feeds") != "https://who.int/rss,https://nih.gov/rss" {
			t.Errorf("unexpected request %s %s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/feed+json")
		_, _ = w.Write([]byte(jsonFeedBody))
	}))
	defer srv.Close()

	c := news.NewClient(srv.URL, srv.Client())
	doc, err := c.Items(context.Background(), []string{"https://who.int/rss", "https://nih.gov/rss"}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 1 {
		t.Fatalf("items = %d, want 1 (linkless item dropped)", len(doc.Items))
	}
	it := doc.Items[0]
	if it.Key != "a" || it.Title != "WHO update" || it.URL != "https://who.int/a" || it.Source != "WHO" || it.FeedURL != "https://who.int/rss" {
		t.Errorf("item = %+v", it)
	}
	if !it.PublishedAt.Equal(time.Date(2026, 9, 17, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("published = %v", it.PublishedAt)
	}
	if len(doc.Warming) != 1 {
		t.Errorf("warming = %v", doc.Warming)
	}
}

func TestClientItemsEmptyFeedsIsNoRequest(t *testing.T) {
	t.Parallel()
	c := news.NewClient("http://127.0.0.1:1", http.DefaultClient)
	doc, err := c.Items(context.Background(), nil, 50)
	if err != nil || len(doc.Items) != 0 {
		t.Errorf("doc=%+v err=%v", doc, err)
	}
}

func TestClientDiscover(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/discover" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["url"] == "https://blocked.example" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"blocked"}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"url":"https://site.example/feed","type":"rss","title":"Site"}]}`))
	}))
	defer srv.Close()

	c := news.NewClient(srv.URL, srv.Client())
	cands, err := c.Discover(context.Background(), "https://site.example")
	if err != nil || len(cands) != 1 || cands[0].URL != "https://site.example/feed" || cands[0].Title != "Site" {
		t.Errorf("cands=%+v err=%v", cands, err)
	}
	if _, err := c.Discover(context.Background(), "https://blocked.example"); err == nil {
		t.Error("400 should be an error")
	}
}

func TestClientUpstreamErrorSurfaces(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := news.NewClient(srv.URL, srv.Client())
	if _, err := c.Items(context.Background(), []string{"https://x/rss"}, 10); err == nil {
		t.Error("502 should be an error")
	}
}
