package awards

import (
	"context"
	"sort"

	"github.com/NorthAIProject/north-client/internal/awards/award"
)

// ProjectMonthlyMedal is a met month as an award on the wire.
func ProjectMonthlyMedal(ctx context.Context, m MonthlyMedal) award.View {
	return award.MonthlyMedal(ctx, m.Key, string(m.Metric), m.Target, m.At)
}

// ProjectMonthlyChallenge is the current month's challenge on the wire. Met is
// the live answer, so it may be true a moment before the medal exists.
func ProjectMonthlyChallenge(ctx context.Context, c MonthlyChallenge) award.MonthlyChallengeView {
	return award.MonthlyChallenge(ctx, c.Key, string(c.Metric), c.Target, c.Progress, c.Met)
}

// ProjectMonthlyMedals projects every medal.
func ProjectMonthlyMedals(ctx context.Context, medals []MonthlyMedal) []award.View {
	out := make([]award.View, 0, len(medals))
	for _, m := range medals {
		out = append(out, ProjectMonthlyMedal(ctx, m))
	}
	return out
}

// MergeEarned puts monthly medals among the badges, oldest first, as the badges alone always went out. It never
// reads the catalog, so held/total counts stay badges only.
func MergeEarned(badges, medals []award.View) []award.View {
	out := make([]award.View, 0, len(badges)+len(medals))
	out = append(out, badges...)
	out = append(out, medals...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].EarnedAt, out[j].EarnedAt
		return a != nil && b != nil && a.Before(*b)
	})
	return out
}
