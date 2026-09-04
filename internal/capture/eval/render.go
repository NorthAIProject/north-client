package eval

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/users"
)

// defaultTimezone is a real, non-UTC zone. Grading against UTC would let a
// prompt that dropped the timezone entirely still pass.
const defaultTimezone = "Europe/Lisbon"

// User builds the account a case runs as.
func (c Case) User() users.User {
	zone := c.Timezone
	if zone == "" {
		zone = defaultTimezone
	}
	return users.User{DisplayName: "Fernando", Timezone: zone, Tier: users.TierFree}
}

// Known builds the habit records a case's names stand for.
//
// Only the names matter to the prompt; the parser is handed the same records
// production hands it, so the fixture stays the shape the application uses.
func (c Case) Known() []habits.Habit {
	known := make([]habits.Habit, 0, len(c.Habits))
	for _, name := range c.Habits {
		known = append(known, habits.Habit{Name: name})
	}
	return known
}

// RenderFor builds the prompt a case would send, through the production
// renderer. Both tiers call this, which is what keeps them grading the same
// thing.
func RenderFor(t *testing.T, c Case) string {
	t.Helper()

	user := c.User()
	system, err := capture.RenderPrompt(user, c.Text, c.Known(), time.Now().In(user.Location()))
	if err != nil {
		t.Fatalf("%s: render prompt: %v", c.ID, err)
	}
	return system
}
