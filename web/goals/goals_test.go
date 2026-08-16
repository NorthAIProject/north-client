package goals

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/users"
)

func TestIndexEmptyStateHasAPrimaryCTA(t *testing.T) {
	var b strings.Builder
	err := IndexPage(users.User{DisplayName: "Ada"}, nil, Instruments{}, GoalForm{}).Render(context.Background(), &b)
	if err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if !strings.Contains(html, "Nothing to aim at yet") {
		t.Fatal("goals empty state missing title")
	}
	if !strings.Contains(html, "The coach works better once it knows what you are aiming at") {
		t.Fatal("goals empty state missing body")
	}
	if !strings.Contains(html, "Add a goal") {
		t.Fatal("goals empty state missing primary CTA")
	}
	if !strings.Contains(html, "open = true") {
		t.Fatal("empty CTA does not open the form")
	}
}
