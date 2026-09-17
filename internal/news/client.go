package news

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxFeedsPerCall is the aggregator's cap on one /v1/items request.
const maxFeedsPerCall = 25

// Aggregator is the feeds service as this slice sees it. Infrastructure:
// it moves bytes and holds no business logic.
type Aggregator interface {
	Items(ctx context.Context, feeds []string, limit int) (Document, error)
	Discover(ctx context.Context, siteURL string) ([]Candidate, error)
}

// Client talks JSON Feed 1.1 to the shared aggregator. Standard library only.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a client; a nil http.Client gets a 15 s timeout default.
func NewClient(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

type jsonFeed struct {
	Items []struct {
		ID            string    `json:"id"`
		URL           string    `json:"url"`
		Title         string    `json:"title"`
		DatePublished time.Time `json:"date_published"`
		Feeds         struct {
			SourceName string `json:"source_name"`
			FeedURL    string `json:"feed_url"`
		} `json:"_feeds"`
	} `json:"items"`
	Feeds struct {
		Warming []string `json:"warming"`
		Stale   []string `json:"stale"`
	} `json:"_feeds"`
}

// Items fetches the merged timeline. Items without a link are dropped: a
// ticker row you cannot open is noise.
func (c *Client) Items(ctx context.Context, feeds []string, limit int) (Document, error) {
	if len(feeds) == 0 {
		return Document{}, nil
	}
	if len(feeds) > maxFeedsPerCall {
		feeds = feeds[:maxFeedsPerCall]
	}
	q := url.Values{"feeds": {strings.Join(feeds, ",")}, "limit": {strconv.Itoa(limit)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/items?"+q.Encode(), nil)
	if err != nil {
		return Document{}, err
	}
	req.Header.Set("Accept", "application/feed+json, application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Document{}, fmt.Errorf("feeds items: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Document{}, fmt.Errorf("feeds items: status %d", resp.StatusCode)
	}
	var body jsonFeed
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return Document{}, fmt.Errorf("feeds items: decode: %w", err)
	}
	doc := Document{Warming: body.Feeds.Warming, Stale: body.Feeds.Stale}
	for _, it := range body.Items {
		title := strings.TrimSpace(it.Title)
		link := strings.TrimSpace(it.URL)
		if title == "" || link == "" {
			continue
		}
		doc.Items = append(doc.Items, Item{
			Key: it.ID, Title: title, URL: link, Source: strings.TrimSpace(it.Feeds.SourceName),
			FeedURL: it.Feeds.FeedURL, PublishedAt: it.DatePublished.UTC(),
		})
	}
	return doc, nil
}

// Discover asks the aggregator which feeds sit behind a site URL. A 400 means
// the aggregator refused the URL (private address, bad scheme); that surfaces
// as an error the service turns into a field message.
func (c *Client) Discover(ctx context.Context, siteURL string) ([]Candidate, error) {
	payload, _ := json.Marshal(map[string]string{"url": siteURL})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/discover", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feeds discover: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feeds discover: status %d", resp.StatusCode)
	}
	var body struct {
		Candidates []Candidate `json:"candidates"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("feeds discover: decode: %w", err)
	}
	return body.Candidates, nil
}
