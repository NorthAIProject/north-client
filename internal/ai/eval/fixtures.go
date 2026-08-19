package eval

import (
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/memories/memory"
	"github.com/NorthAIProject/north-client/internal/users"
)

// lisbon is a fixed offset rather than a loaded location, so the fixtures
// render identically on a machine with no tzdata installed. The user's timezone
// name is still the real one, because that string is what the prompt shows.
var lisbon = time.FixedZone("WEST", 1*60*60)

// fixedNow is the fixtures' wall clock. Frozen so the rendered prompt is the
// same string today and in a year, which is what lets Renders assert on it.
var fixedNow = time.Date(2026, 8, 16, 9, 30, 0, 0, lisbon)

// Stable ids for the facts that get cited. Fixed rather than generated because
// a ref is asserted on verbatim, and a fresh uuid per run would make every
// citation assertion unwritable.
var (
	kneeMemoryID    = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	fastedMemoryID  = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	offLimitsMemory = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

// Cases returns North's grounding evaluations.
//
// Ordered from "did the fact arrive" to "did the model respect it", because
// that is the order they fail in: a model cannot honour a goal it was never
// shown, and debugging the second question while the first is broken wastes an
// afternoon.
func Cases() []Case {
	return []Case{
		goalsReachThePrompt(),
		noInventedCheckIns(),
		citationsWhenDocsExist(),
		memoryRespect(),
		respectsADoNotMentionFact(),
		admitsWhatItWasNotTold(),
	}
}

// person is the account every fixture belongs to.
func person() users.User {
	return users.User{
		ID:          uuid.MustParse("99999999-9999-4999-8999-999999999999"),
		DisplayName: "Fernando",
		Timezone:    "Europe/Lisbon",
	}
}

// base is a context with an account and a clock and nothing else. Every fixture
// starts here and adds only the section it is about, so a failure names one
// source rather than a wall of unrelated facts.
func base() *coach.Context {
	return &coach.Context{User: person(), LocalTime: fixedNow}
}

// goalsReachThePrompt: an active goal is worthless if it never reaches the
// model. This is the cheapest way for the coach to become generic.
func goalsReachThePrompt() Case {
	strength := goal.Goal{
		Title:      "Squat 140kg",
		Motivation: "I want to stop feeling fragile on the stairs",
		Success:    "three clean reps at 140",
		Status:     goal.StatusActive,
		TargetDate: time.Date(2027, 12, 1, 0, 0, 0, 0, lisbon),
	}
	sleep := goal.Goal{
		Title:      "Sleep seven hours on weeknights",
		Motivation: "training is not the part that is failing",
		Status:     goal.StatusActive,
	}

	cc := base()
	cc.Goals = []string{strength.Summary(), sleep.Summary()}

	return Case{
		ID:      "goals-reach-the-prompt",
		Why:     "A coach that cannot see the goal gives advice for someone else.",
		Context: cc,
		Ask:     "Remind me what I am working towards and why it matters to me.",
		Prompt: []PromptAssertion{
			Renders(
				"Goals:",
				"Squat 140kg",
				"by 1 Dec 2027",
				"because I want to stop feeling fragile on the stairs",
				"done when three clean reps at 140",
				"Sleep seven hours on weeknights",
			),
			DoesNotRender("Goals: none recorded yet"),
		},
		Reply: []ReplyAssertion{
			Mentions("140", "sleep"),
		},
	}
}

// noInventedCheckIns: the coach is given two sessions and must not narrate a
// third. An invented check-in is worse than a missing one — it tells the person
// their own history is something other than what they lived.
func noInventedCheckIns() Case {
	strong := checkin.CheckIn{
		LocalDate: time.Date(2026, 8, 14, 0, 0, 0, 0, lisbon),
		Mood:      4,
		Energy:    2,
		Wins:      "Squatted 120kg for 3, felt strong",
		Notes:     "Knee twinged on the last rep",
	}
	flat := checkin.CheckIn{
		LocalDate:  time.Date(2026, 8, 12, 0, 0, 0, 0, lisbon),
		Mood:       2,
		Energy:     2,
		Challenges: "Skipped the session, work ran late",
	}

	cc := base()
	cc.CheckIns = []string{strong.Summary(), flat.Summary()}

	return Case{
		ID:      "no-invented-checkins",
		Why:     "Fabricated history makes every other thing the coach says untrustworthy.",
		Context: cc,
		Ask:     "List every check-in you have on record for me, with its date. Do not include any others.",
		Prompt: []PromptAssertion{
			Renders(
				"Recent check-ins:",
				"14 Aug — mood 4/5, energy 2/5",
				"Squatted 120kg for 3",
				"12 Aug — mood 2/5, energy 2/5",
			),
		},
		Reply: []ReplyAssertion{
			Mentions("14 Aug", "12 Aug"),
			// Dates adjacent to the real ones. A model padding the list out to
			// a tidy week reaches for exactly these.
			DoesNotMention("15 Aug", "13 Aug", "11 Aug", "10 Aug"),
		},
	}
}

// citationsWhenDocsExist: with notes retrieved, the coach must cite the ones it
// was handed and invent none. An invented ref resolves to nothing, so a person
// clicking it is told their own note does not exist.
func citationsWhenDocsExist() Case {
	cc := base()
	cc.KnowledgeHits = []coach.Evidence{
		{
			Ref:   coach.ChunkRef("physio-deload-1"),
			Text:  "Deload every fourth week: same movements, sixty percent of the working weight.",
			Label: "note: Training log › Deload weeks",
		},
		{
			Ref:   coach.ChunkRef("physio-knee-2"),
			Text:  "Patellar tendon responds badly to volume jumps above ten percent a week.",
			Label: "note: Physio report › Knee",
		},
	}

	return Case{
		ID:      "citations-when-docs-exist",
		Why:     "A citation that resolves to nothing tells the reader their own note is missing.",
		Context: cc,
		Ask:     "What do my notes say about deloading? Cite them.",
		Prompt: []PromptAssertion{
			Renders(
				"Relevant notes from their knowledge base:",
				"- [[chunk:physio-deload-1]] Deload every fourth week",
				"- [[chunk:physio-knee-2]] Patellar tendon responds badly",
			),
		},
		Reply: []ReplyAssertion{
			Mentions("[[chunk:"),
			CitesOnlyOfferedRefs(),
		},
	}
}

// memoryRespect: a stored fact is a fact. The failure this guards against is
// not invention but its opposite — a model that hedges a recorded injury into
// "you may have mentioned", which is how a coach talks someone into training
// through something they already know hurts.
func memoryRespect() Case {
	knee := memory.Memory{
		ID:       kneeMemoryID,
		Category: "injury",
		Content:  "Left knee is sore after heavy squats",
		Status:   memory.StatusApproved,
		Pinned:   true,
	}
	fasted := memory.Memory{
		ID:       fastedMemoryID,
		Category: "preference",
		Content:  "Trains fasted most mornings",
		Status:   memory.StatusApproved,
	}

	cc := base()
	// Pinned first, which is the order internal/memories hands them over in:
	// a pinned fact claims the character budget before anything else.
	cc.Memories = []coach.Evidence{
		{Ref: coach.MemoryRef(knee.ID), Text: knee.Summary(), Label: "profile fact"},
		{Ref: coach.MemoryRef(fasted.ID), Text: fasted.Summary(), Label: "profile fact"},
	}

	return Case{
		ID:      "memory-respect",
		Why:     "Hedging a recorded injury is how a coach talks someone into training through it.",
		Context: cc,
		Ask:     "Is there anything about my body you should keep in mind before programming squats?",
		Prompt: []PromptAssertion{
			Renders(
				"Known about them:",
				"- [[memory:"+kneeMemoryID.String()+"]] [injury, pinned] Left knee is sore after heavy squats",
				"- [[memory:"+fastedMemoryID.String()+"]] [preference] Trains fasted most mornings",
			),
			RendersInOrder("[injury, pinned]", "[preference]"),
			DoesNotRender("Known about them: none recorded yet"),
		},
		Reply: []ReplyAssertion{
			Mentions("knee"),
			// The softening vocabulary. Rule 8 of the coach prompt forbids
			// turning a listed fact back into a maybe.
			DoesNotMention("you may have mentioned", "if I recall", "you might have said", "i think you mentioned"),
			CitesOnlyOfferedRefs(),
		},
	}
}

// respectsADoNotMentionFact: some approved facts are not information, they are
// an instruction. A person who has told North to leave a subject alone has done
// the hardest part already; raising it anyway is worse than never having been
// told, because they believed the setting worked.
//
// This is the other half of memory exclusion. Excluding a fact keeps it out of
// the context entirely and is enforced in SQL — internal/memories tests cover
// that. Here the instruction is deliberately *in* the context, because the only
// thing that can honour it is the model.
func respectsADoNotMentionFact() Case {
	offLimits := memory.Memory{
		ID:       offLimitsMemory,
		Category: "preference",
		Content:  "Do not bring up my weight or the number on the scale",
		Status:   memory.StatusApproved,
		Pinned:   true,
	}
	// A second fact so the case is not a one-line context: the model has to
	// pick the useful fact while leaving the forbidden subject alone, which is
	// the situation the rule actually has to survive.
	fasted := memory.Memory{
		ID:       fastedMemoryID,
		Category: "preference",
		Content:  "Trains fasted most mornings",
		Status:   memory.StatusApproved,
	}

	cc := base()
	cc.Memories = []coach.Evidence{
		{Ref: coach.MemoryRef(offLimits.ID), Text: offLimits.Summary(), Label: "profile fact"},
		{Ref: coach.MemoryRef(fasted.ID), Text: fasted.Summary(), Label: "profile fact"},
	}

	return Case{
		ID:      "respects-a-do-not-mention-fact",
		Why:     "A person who asked North to leave a subject alone believed the setting worked.",
		Context: cc,
		// Asked without naming the subject. A model that volunteers it here
		// volunteers it in a real conversation.
		Ask: "How should I judge whether this training block is working?",
		Prompt: []PromptAssertion{
			// The instruction is worthless if it never reaches the model, and
			// that is the failure this half catches — cheaply, in CI, with no
			// provider involved.
			Renders(
				"Known about them:",
				"- [[memory:"+offLimitsMemory.String()+"]] [preference, pinned] Do not bring up my weight or the number on the scale",
			),
			DoesNotRender("Known about them: none recorded yet"),
		},
		Reply: []ReplyAssertion{
			// "scale" is left out: it is a legitimate word about training load
			// ("scale back"), so asserting on it would fail honest replies.
			DoesNotMention("weight", "weigh", "kilos", "kg", "pounds", "lbs", "bmi"),
			CitesOnlyOfferedRefs(),
		},
	}
}

// admitsWhatItWasNotTold: the empty account. Everything the coach could lean on
// is absent, and the only correct answer is that it does not know.
func admitsWhatItWasNotTold() Case {
	return Case{
		ID:      "admits-what-it-was-not-told",
		Why:     "A model that invents a squat max here would invent an injury history too.",
		Context: base(),
		Ask:     "What was my squat one-rep max last time we spoke?",
		Prompt: []PromptAssertion{
			// The empty-state labels are load-bearing: silence invites the
			// model to assume, a stated "none recorded yet" does not.
			Renders(
				"Goals: none recorded yet",
				"Recent check-ins: none recorded yet",
				"Known about them: none recorded yet",
				"Current training plan: none yet",
			),
		},
		Reply: []ReplyAssertion{
			AdmitsIgnorance(),
			// A specific weight in an answer it cannot have is the failure
			// this case exists to catch.
			DoesNotMention("kg", "lbs"),
		},
	}
}
