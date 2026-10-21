package landing

import (
	"context"
	"strings"
	"testing"
)

// Each store badge has three possible states, and going live on a store is a
// one-line change to a constant. These pin what each state promises.
func TestAppleBadgeStates(t *testing.T) {
	ctx := context.Background()

	live := appleBadge(ctx, "https://apps.apple.com/app/id1", "https://testflight.apple.com/join/x")
	if live.Href != "https://apps.apple.com/app/id1" || live.Muted || live.Caption != "" {
		t.Errorf("a listing should link without a caption, got %+v", live)
	}

	beta := appleBadge(ctx, "", "https://testflight.apple.com/join/x")
	if beta.Href != "https://testflight.apple.com/join/x" || beta.Muted || beta.Caption != "Beta · TestFlight" {
		t.Errorf("TestFlight only should link to the invite with a beta caption, got %+v", beta)
	}

	none := appleBadge(ctx, "", "")
	if none.Href != "" || !none.Muted || none.Caption != "Coming soon" {
		t.Errorf("no iOS link should mute the badge, got %+v", none)
	}
}

func TestPlayBadgeStates(t *testing.T) {
	ctx := context.Background()

	if live := playBadge(ctx, "https://play.google.com/store/apps/details?id=x"); live.Muted || live.Href == "" {
		t.Errorf("a listing should link, got %+v", live)
	}
	soon := playBadge(ctx, "")
	if soon.Href != "" || !soon.Muted || soon.Caption != "Coming soon" || soon.Aria != "Google Play — coming soon" {
		t.Errorf("no listing should render muted and say so, got %+v", soon)
	}
}

// Today: Apple links to TestFlight, Google Play is not a link at all.
func TestLandingRendersTodaysStoreBadges(t *testing.T) {
	var b strings.Builder
	if err := Page().Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	for _, want := range []string{
		`href="` + iosBetaURL + `"`,
		"/assets/brand/badges/download-on-the-app-store.svg",
		"/assets/brand/badges/google-play-badge.png",
		"Beta · TestFlight",
		`aria-label="Google Play — coming soon"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("landing is missing %q", want)
		}
	}
	if strings.Contains(out, "play.google.com") {
		t.Error("the Play badge links somewhere, but there is no listing")
	}
}

// The card wall is decoration: hidden from assistive technology and holding
// nothing a keyboard can land on.
func TestHeroWallIsDecorative(t *testing.T) {
	var b strings.Builder
	if err := heroWall().Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	if !strings.HasPrefix(strings.TrimSpace(out), `<div aria-hidden="true" class="hero-wall">`) {
		t.Error("the wall is not aria-hidden")
	}
	for _, focusable := range []string{"<a ", "<button", "<input", "tabindex"} {
		if strings.Contains(out, focusable) {
			t.Errorf("the wall contains focusable %q", focusable)
		}
	}
	if got := strings.Count(out, `class="hero-wall-slot`); got != len(wallFront)+len(wallBack) {
		t.Errorf("wall renders %d cards, want %d", got, len(wallFront)+len(wallBack))
	}
}
