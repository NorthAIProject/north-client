package dashboard

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// NextStep is the one action a fresh account should take next.
//
// The dashboard already composes today's home. This is the same question
// asked of a person who has not started yet: do not show them eight empty
// panels and hope they pick one.
type NextStep struct {
	Kind    string
	Eyebrow string
	Title   string
	Body    string
	CTA     string
	Href    string
}

const (
	stepKindGoal    = "goal"
	stepKindCheckIn = "checkin"
	stepKindChat    = "chat"

	// StepKindPush is the one step past activation: let North reach this
	// phone. Exported because the template treats it differently — the card
	// hides itself when the browser cannot deliver, which only script can know.
	StepKindPush = "push"
)

// nextSteps maps each step's kind to where it leads. Its words live in the
// catalogue under "dash.step.<kind>.*": the native app shows them verbatim,
// so they are read in the caller's language rather than stored here.
var nextSteps = map[string]string{
	stepKindGoal:    "/app/goals",
	stepKindCheckIn: "/app/check-ins",
	stepKindChat:    "/app/chat",
	StepKindPush:    "/app/settings#notifications",
}

// nextStep builds the card for kind in ctx's language.
func nextStep(ctx context.Context, kind string) NextStep {
	key := "dash.step." + kind + "."
	return NextStep{
		Kind:    kind,
		Eyebrow: i18n.T(ctx, key+"eyebrow"),
		Title:   i18n.T(ctx, key+"title"),
		Body:    i18n.T(ctx, key+"body"),
		CTA:     i18n.T(ctx, key+"cta"),
		Href:    nextSteps[kind],
	}
}

// PickNextStep returns the first missing activation step. Goal, then today's
// check-in, then a coach thread, then — once all three are there — the offer
// to be nudged on this device. Returning users who have everything see nothing.
// The words are in ctx's language; Kind and Href never change with it.
//
// Push comes last rather than first because a nudge is only worth receiving
// once there is something to be nudged about, and because asking for
// notification permission before somebody has seen the product is how the
// permission gets refused for good.
func PickNextStep(ctx context.Context, s Snapshot) (NextStep, bool) {
	switch {
	case len(s.Goals) == 0:
		return nextStep(ctx, stepKindGoal), true
	case !s.CheckedInToday:
		return nextStep(ctx, stepKindCheckIn), true
	case s.LastThread == nil:
		return nextStep(ctx, stepKindChat), true
	case s.PushOffered:
		return nextStep(ctx, StepKindPush), true
	default:
		return NextStep{}, false
	}
}
