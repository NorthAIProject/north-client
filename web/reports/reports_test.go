package reports

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/reports/report"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestIndexEmptyStateHasAGenerateCTA(t *testing.T) {
	var b strings.Builder
	err := IndexPage(users.User{DisplayName: "Ada"}, nil, IndexView{}).Render(context.Background(), &b)
	if err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if !strings.Contains(html, "No review yet") {
		t.Fatal("reports empty state missing title")
	}
	if !strings.Contains(html, "from what you actually recorded") {
		t.Fatal("reports empty state missing body")
	}
	if strings.Count(html, `action="/app/reports/generate"`) < 2 {
		t.Fatal("empty reports page does not repeat the generate form in the empty state")
	}
}

func TestIndexWithReportsDoesNotShowEmptyState(t *testing.T) {
	var b strings.Builder
	err := IndexPage(users.User{DisplayName: "Ada"}, []report.Report{{Title: "Week of 4 Aug"}}, IndexView{}).Render(context.Background(), &b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "No review yet") {
		t.Fatal("populated reports page still shows the empty state")
	}
}
