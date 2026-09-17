package newspages

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/news/item"
)

func render(t *testing.T, items []item.Item) string {
	t.Helper()
	var b strings.Builder
	if err := TickerPanel(items, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestTickerPanelRendersHeadlineSourceAndLink(t *testing.T) {
	html := render(t, []item.Item{{
		Key: "a", Title: "Sleep study finds", URL: "https://pub.example/a", Source: "NIH",
		PublishedAt: time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC),
	}})
	for _, want := range []string{"Sleep study finds", "NIH", `href="https://pub.example/a"`, `rel="noopener"`, "2h"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
}

func TestTickerPanelEmptyState(t *testing.T) {
	html := render(t, nil)
	if !strings.Contains(html, "news.ticker.empty") && !strings.Contains(html, "No headlines") {
		t.Errorf("empty state missing: %s", html)
	}
}

func TestTickerShellLazyLoadsAndRefreshes(t *testing.T) {
	var b strings.Builder
	if err := TickerShell().Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if !strings.Contains(html, `hx-get="/app/news-ticker"`) || !strings.Contains(html, "every 300s") {
		t.Errorf("shell = %s", html)
	}
}
