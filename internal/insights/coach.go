package insights

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// CoachData is the conversation this person had with the coach over a window.
//
// Counts and ratings only — never the text. The charts here answer "how much
// and how useful", and carrying a year of message bodies through the view
// layer to count them would move a great deal of private text nobody renders.
type CoachData struct {
	Range timerange.Range

	Messages []conversations.MessageStat

	// Truncated marks a window with more turns than one read returns. The
	// page says so rather than quietly charting a partial year.
	Truncated bool
}

// Coach loads the window's conversation turns.
func (s *Service) Coach(ctx context.Context, user users.User, rg timerange.Range) (CoachData, error) {
	out := CoachData{Range: rg}
	if s.conversations == nil {
		return out, nil
	}

	msgs, err := s.conversations.StatsBetween(ctx, user.ID, rg)
	if err != nil {
		return CoachData{}, err
	}

	out.Messages = msgs
	out.Truncated = len(msgs) == conversations.MessageStatLimit
	return out, nil
}
