package settings

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

var contractAt = time.Date(2026, 9, 24, 7, 15, 0, 0, time.UTC)

func TestSettingsShapes(t *testing.T) {
	t.Parallel()

	yes := true
	id := uuid.MustParse("12121212-1212-1212-1212-121212121212")

	apitest.AssertGolden(t, "profile.golden.json", Profile{
		Email: "ana@example.com", DisplayName: "Ana", Timezone: "Europe/Lisbon", Locale: "en",
		CoachingStyle: "Be direct. Skip pep talks. Challenge me.", CoachingTone: "direct",
	})
	apitest.AssertGolden(t, "notifications.golden.json", Notifications{
		NudgeMissedCheckIn: true, TrainingReminders: true, StatsDigestCadence: "weekly",
		QuietHoursEnabled: true, QuietStart: "22:00", QuietEnd: "07:00",
		PhotoAskEnabled: true, PhotoEveryDays: 14, PhotoReminderDays: 2,
	})
	// The key is write-only: the response carries a hint and nothing else.
	apitest.AssertGolden(t, "ai-settings.golden.json", AISettings{
		Enabled:   true,
		Providers: []AIProvider{{Name: "openrouter", Label: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "openai/gpt-4o-mini", KeyHint: "sk-or-…"}},
		Current:   &AICurrent{Provider: "openrouter", KeyHint: "…9f2c", Model: "openai/gpt-4o-mini", SupportsTools: &yes, UpdatedAt: contractAt},
	})
	apitest.AssertGolden(t, "connections.golden.json", ConnectionList{
		Connections:  []Connection{{ID: id, Name: "Laptop", Kind: "claude_code", TokenPrefix: "nk_live_ab12", CreatedAt: contractAt, LastUsedAt: &contractAt}},
		ConnectorURL: "https://kheprios.com/mcp",
	})
	apitest.AssertGolden(t, "connection-created.golden.json", CreatedConnection{
		Connection: Connection{ID: id, Name: "Laptop", Kind: "claude_code", TokenPrefix: "nk_live_ab12", CreatedAt: contractAt},
		Token:      "nk_live_ab12-example-token",
		Setup:      ConnectionSetup{URL: "https://kheprios.com/mcp", ConfigLabel: "Terminal", ConfigLang: "sh", Config: "claude mcp add khepri …"},
	})
	apitest.AssertGolden(t, "activity.golden.json", ActivityList{Executions: []Execution{{
		ID: id, Tool: "log_check_in", Arguments: json.RawMessage(`{"mood":4,"energy":3}`),
		Surface: "coach", Outcome: "executed", CreatedAt: contractAt,
	}}})
	apitest.AssertGolden(t, "telegram.golden.json", TelegramSettings{Enabled: true, BotUsername: "khepri_bot", Linked: true, LinkedAt: &contractAt})
	apitest.AssertGolden(t, "calendar.golden.json", CalendarSettings{Enabled: true, Connected: &CalendarConnection{
		Provider: "mcp", Endpoint: "https://calendar.example.com/mcp", Status: "ok", LastCheckedAt: &contractAt,
	}})
}
