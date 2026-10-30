package quota

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// actionLabel names a quota action the way a person would. An action with no
// catalogue entry falls back to its wire name, which is visibly wrong rather
// than silently English.
func actionLabel(ctx context.Context, action string) string {
	return i18n.T(ctx, "quota.action."+action)
}

// delayLabel renders a wait as something worth reading. "in 47 minutes" is
// useful; "in 2823 seconds" is a number a person has to do arithmetic on.
func delayLabel(ctx context.Context, d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T(ctx, "quota.delay.underminute")
	case d < 2*time.Minute:
		return i18n.T(ctx, "quota.delay.minute")
	case d < time.Hour:
		return i18n.Tf(ctx, "quota.delay.minutes", int(d.Minutes()))
	case d < 2*time.Hour:
		return i18n.T(ctx, "quota.delay.hour")
	default:
		return i18n.Tf(ctx, "quota.delay.hours", int(d.Hours()))
	}
}
