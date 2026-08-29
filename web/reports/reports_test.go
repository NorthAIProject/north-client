package reports

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

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

func TestReportFeedbackAsksThenShowsTheAnswer(t *testing.T) {
	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	render := func(item report.Report) string {
		t.Helper()
		var buf bytes.Buffer
		if err := Feedback(item).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	unrated := render(report.Report{ID: id})
	for _, want := range []string{
		"Was this useful?",
		`id="report-feedback-44444444-4444-4444-4444-444444444444"`,
		`hx-post="/app/reports/44444444-4444-4444-4444-444444444444/helpful"`,
		`action="/app/reports/44444444-4444-4444-4444-444444444444/helpful"`,
		`value="helpful"`,
		`value="unhelpful"`,
		`name="csrf_token"`,
	} {
		if !strings.Contains(unrated, want) {
			t.Errorf("missing %q in:\n%s", want, unrated)
		}
	}

	yes := true
	rated := render(report.Report{ID: id, Helpful: &yes})
	if !strings.Contains(rated, "Marked useful") {
		t.Errorf("a rated report does not show its answer:\n%s", rated)
	}
	if strings.Contains(rated, "Was this useful?") {
		t.Error("a rated report still asks the question")
	}
	if !strings.Contains(rated, `value="clear"`) {
		t.Errorf("no way to undo:\n%s", rated)
	}
}

// The question only means anything once there is something to read, so a report
// still being written must not ask it.
func TestAReportWithNoBodyDoesNotAskForFeedback(t *testing.T) {
	var buf bytes.Buffer
	err := DetailPage(users.User{DisplayName: "Fernando"}, report.Report{
		ID:     uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Title:  "Week of 10 Aug 2026",
		Status: report.StatusPending,
	}, "").Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Was this useful?") {
		t.Error("a pending report is asking whether it was useful")
	}
}
