package coach

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	chatpages "github.com/NorthAIProject/north-client/web/chat"
)

// buildCoachStats derives sidebar instrumentation from the conversation list.
//
// MessagesThisWeek is approximate: for threads touched since the start of the
// calendar week, message counts are summed when available; otherwise each active
// thread counts as one.
func buildCoachStats(ctx context.Context, convos *conversations.Service, list []conversations.Conversation, now time.Time) (chatpages.CoachStats, error) {
	weekStart := timerange.StartOfWeek(now)
	stats := chatpages.CoachStats{ThreadCount: len(list)}
	for _, c := range list {
		if c.UpdatedAt.Before(weekStart) {
			continue
		}
		n, err := convos.CountMessages(ctx, c.ID)
		if err != nil {
			return chatpages.CoachStats{}, err
		}
		if n > 0 {
			stats.MessagesThisWeek += n
		} else {
			stats.MessagesThisWeek++
		}
	}
	return stats, nil
}
