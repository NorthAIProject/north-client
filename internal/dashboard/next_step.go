package dashboard

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

var (
	stepGoal = NextStep{
		Kind:    stepKindGoal,
		Eyebrow: "Start here",
		Title:   "Name one thing you are working toward",
		Body:    "Khepri reads your goals before every reply. One is enough.",
		CTA:     "Add a goal",
		Href:    "/app/goals",
	}
	stepCheckIn = NextStep{
		Kind:    stepKindCheckIn,
		Eyebrow: "Today",
		Title:   "How did today go?",
		Body:    "Thirty seconds. Mood, energy, what went well.",
		CTA:     "Check in",
		Href:    "/app/check-ins",
	}
	stepChat = NextStep{
		Kind:    stepKindChat,
		Eyebrow: "Coach",
		Title:   "Talk to Khepri about the week",
		Body:    "It already has your goals. Start with what is in the way.",
		CTA:     "Open the coach",
		Href:    "/app/chat",
	}
	stepPush = NextStep{
		Kind:    StepKindPush,
		Eyebrow: "Stay on track",
		Title:   "Let Khepri reach you",
		Body:    "A short note on this device when you have not checked in, a goal is due, or it is a training day.",
		CTA:     "Turn on nudges",
		Href:    "/app/settings#notifications",
	}
)

// PickNextStep returns the first missing activation step. Goal, then today's
// check-in, then a coach thread, then — once all three are there — the offer
// to be nudged on this device. Returning users who have everything see nothing.
//
// Push comes last rather than first because a nudge is only worth receiving
// once there is something to be nudged about, and because asking for
// notification permission before somebody has seen the product is how the
// permission gets refused for good.
func PickNextStep(s Snapshot) (NextStep, bool) {
	switch {
	case len(s.Goals) == 0:
		return stepGoal, true
	case !s.CheckedInToday:
		return stepCheckIn, true
	case s.LastThread == nil:
		return stepChat, true
	case s.PushOffered:
		return stepPush, true
	default:
		return NextStep{}, false
	}
}
