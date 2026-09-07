// Package analytics decides what the browser is allowed to report on the page
// being rendered.
//
// Separate from internal/analytics, which is the server-side funnel. That one
// chooses which events to send; this one chooses what the visitor's own browser
// may observe, which is a different question with a different answer per page.
//
// The decisions live in Go rather than in the snippet so they can be tested and
// reviewed as a list. A page added to the wrong side of one of these lines
// leaks something, and "read the regex in the template" is not a review.
package analytics

import (
	"context"
	"strings"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Config is what the snippet hands to posthog.init, as JSON.
//
// Every field is a decision made server-side. The browser script reads this
// and calls posthog; it decides nothing on its own.
type Config struct {
	APIKey string `json:"apiKey"`
	Host   string `json:"host"`

	// Identity is the signed-in account id, matching the server's DistinctId.
	// Empty for a visitor, and the snippet then leaves the browser anonymous.
	Identity string `json:"identity,omitempty"`

	// Autocapture records clicks and their element text. Only ever true on the
	// public pages: inside /app the text of a clicked element is a goal title,
	// a memory, or a check-in note.
	Autocapture bool `json:"autocapture"`

	// Record turns on session replay for this page.
	Record bool `json:"record"`

	// KeepQuery allows the full URL, query string included, into
	// $current_url. False strips it.
	KeepQuery bool `json:"keepQuery"`
}

// autocapturePages are the signed-out pages where recording which element
// somebody clicked is safe and useful.
//
// Deliberately a fixed set rather than "anything outside /app". The password
// reset pages are signed out too, and their URLs carry a single-use token that
// must not reach an analytics property.
var autocapturePages = map[string]bool{
	"/":                true,
	"/signup":          true,
	"/login":           true,
	"/privacy":         true,
	"/terms":           true,
	"/oauth/authorize": true,
}

// recordPages are the pages where session replay is on.
//
// Two, and both signed out. The privacy policy promises that no message
// content, document text, or health data is recorded, and replay of any page
// under /app would break that promise by definition. These two are the pages
// where "where did they hesitate" is a question nothing else can answer.
var recordPages = map[string]bool{
	"/":                true,
	"/oauth/authorize": true,
}

// Resolve builds the configuration for the page being rendered.
//
// A zero Config with an empty APIKey means "render no snippet", which is what
// a deployment with no PostHog key gets.
func Resolve(ctx context.Context) Config {
	cfg := middleware.AnalyticsFrom(ctx)
	if !cfg.Enabled() {
		return Config{}
	}

	path := pathOf(middleware.Path(ctx))

	return Config{
		APIKey:      cfg.APIKey,
		Host:        cfg.Host,
		Identity:    middleware.IdentityFrom(ctx),
		Autocapture: autocapturePages[path],
		Record:      recordPages[path],

		// The landing page only. Its query string is the campaign that brought
		// somebody — utm_source, a ref code — and dropping it would throw away
		// the attribution this whole exercise exists to collect. Everywhere
		// else the query is a liability: /oauth/authorize carries a
		// request_id, /reset-password a single-use token, and neither belongs
		// in an analytics property.
		KeepQuery: path == "/",
	}
}

// Enabled reports whether there is a snippet to render at all.
func (c Config) Enabled() bool { return c.APIKey != "" }

// pathOf drops the query string and any trailing slash, so the lookups above
// compare against one spelling of a page.
//
// middleware.Path carries the full RequestURI because the language switcher
// needs to post back to it. Everything here is a decision about the page, not
// the request.
func pathOf(requestURI string) string {
	path := requestURI
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	if path == "" {
		return "/"
	}
	return path
}
