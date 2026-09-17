// Package news is the breaking-news ticker: headlines the worker sweeps from
// the cluster's shared feed aggregator into Postgres, a per-user switch, and
// the feeds a person follows on top of the curated list.
//
// Headline, source, time and link only. No bodies are fetched or stored; the
// ticker links out to the publisher.
package news

import "github.com/NorthAIProject/north-client/internal/news/item"

// Item and UserFeed live in the leaf package so templates can render them
// without importing this one.
type (
	Item     = item.Item
	UserFeed = item.UserFeed
)

// Ticker is what the dashboard renders.
type Ticker struct {
	Enabled bool
	Items   []Item
}

// Candidate is a feed the aggregator found behind a site URL.
type Candidate struct {
	URL   string `json:"url"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// Document is the aggregator's merged timeline plus which requested feeds
// are still warming up or serving stale items.
type Document struct {
	Items   []Item
	Warming []string
	Stale   []string
}
