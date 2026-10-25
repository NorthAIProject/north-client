package award

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// View is a badge on the wire, named and described in the request's locale so
// the native apps ship no badge strings. It lives here, beside the catalog,
// so the feed, the profile and the awards API all project it one way.
type View struct {
	ID          string `json:"id"`
	Area        string `json:"area"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// EarnedAt is absent on a badge still to earn.
	EarnedAt *time.Time `json:"earnedAt,omitempty"`
	// Progress is the count toward Target, sent on a badge still to earn.
	Progress *int `json:"progress,omitempty"`
	Target   int  `json:"target"`
	// Rarity is the share of active people who hold it, such as "12%" or
	// "<1%"; absent when it is not known.
	Rarity string `json:"rarity,omitempty"`
	// Monthly is set on a monthly medal (area "monthly"), which is not a
	// badge; clients draw its coin from this rather than from the area.
	Monthly *MonthlyMedalView `json:"monthly,omitempty"`
}

// Name is the badge's name in the request's locale.
func (b Badge) Name(ctx context.Context) string { return i18n.T(ctx, "awards.badge."+b.ID+".name") }

// Description is what earning the badge takes, in the request's locale.
func (b Badge) Description(ctx context.Context) string {
	return i18n.T(ctx, "awards.badge."+b.ID+".desc")
}

// Project is the badge as a View; earnedAt, progress and rarity are optional.
func (b Badge) Project(ctx context.Context, earnedAt *time.Time, progress *int, rarity string) View {
	return View{
		ID: b.ID, Area: string(b.Area), Name: b.Name(ctx), Description: b.Description(ctx),
		EarnedAt: earnedAt, Progress: progress, Target: b.Target, Rarity: rarity,
	}
}
