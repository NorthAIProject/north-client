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
)

var (
	stepGoal = NextStep{
		Kind:    stepKindGoal,
		Eyebrow: "Start here",
		Title:   "Name one thing you are working toward",
		Body:    "North reads your goals before every reply. One is enough.",
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
		Title:   "Talk to North about the week",
		Body:    "It already has your goals. Start with what is in the way.",
		CTA:     "Open the coach",
		Href:    "/app/chat",
	}
)

// PickNextStep returns the first missing activation step. Goal, then today's
// check-in, then a coach thread. Returning users who have all three see nothing.
func PickNextStep(s Snapshot) (NextStep, bool) {
	switch {
	case len(s.Goals) == 0:
		return stepGoal, true
	case !s.CheckedInToday:
		return stepCheckIn, true
	case s.LastThread == nil:
		return stepChat, true
	default:
		return NextStep{}, false
	}
}
