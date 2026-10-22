package workoutcard

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// topExercises is how many movements a card names.
const topExercises = 2

func top(c lift.Card) []lift.CardExercise {
	if len(c.Exercises) > topExercises {
		return c.Exercises[:topExercises]
	}
	return c.Exercises
}

// stats are the card's numbers in reading order: time, sets, volume, then
// distance. A part that is zero is left out rather than shown as "0 sets".
func stats(ctx context.Context, c lift.Card) []string {
	var out []string
	if m := int(math.Round(c.Duration.Minutes())); m > 0 {
		out = append(out, i18n.Tf(ctx, "workoutcard.minutes", m))
	}
	switch {
	case c.Sets == 1:
		out = append(out, i18n.T(ctx, "workoutcard.set"))
	case c.Sets > 1:
		out = append(out, i18n.Tf(ctx, "workoutcard.sets", c.Sets))
	}
	if c.VolumeKg > 0 {
		out = append(out, i18n.Tf(ctx, "workoutcard.volume", thousands(int(math.Round(c.VolumeKg)))))
	}
	if c.DistanceM != nil && *c.DistanceM > 0 {
		// One decimal at most, trimmed: "5 km", "5.2 km".
		km := math.Round(*c.DistanceM/100) / 10
		prec := 1
		if km == math.Trunc(km) {
			prec = 0
		}
		out = append(out, i18n.Tf(ctx, "workoutcard.km", i18n.Decimal(ctx, km, prec)))
	}
	return out
}

func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
