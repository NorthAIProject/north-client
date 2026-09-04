package eval

import "github.com/NorthAIProject/north-client/internal/capture/captured"

// theirHabits is the habit list most cases are grounded in.
//
// Two of them start with "morning" on purpose: an ambiguous name is a question
// the preview asks, and a model that resolves it silently has taken a decision
// that is not its to take.
var theirHabits = []string{"Read 20 pages", "Cold shower", "Morning walk", "Morning pages"}

// Cases is the corpus.
//
// Every one of these is a sentence somebody actually types. They are weighted
// towards refusal rather than extraction, because extraction failures are loud
// — a mangled unit is refused by Validate and the person sees the leftovers —
// and refusal failures are silent. An invented energy score is a plausible
// number in a log nobody re-reads.
func Cases() []Case {
	return []Case{
		{
			ID:     "the-whole-morning",
			Why:    "The sentence the feature exists for. If this does not work, nothing else matters.",
			Text:   "slept 6h, 2L water, read 20 pages, 78kg, mood 4 energy 3",
			Habits: theirHabits,
			Prompt: []PromptAssertion{ListsTheirHabits()},
			Draft: []DraftAssertion{
				Counts(5),
				Sleep(360),
				Water(2000),
				HabitNamed("Read 20 pages"),
				WeightKG(78),
				Feels(4, 3),
			},
		},
		{
			ID:     "half-a-check-in",
			Why:    "Inventing the other half writes a feeling the person never reported.",
			Text:   "mood 4",
			Habits: theirHabits,
			Draft: []DraftAssertion{
				// A check-in needs both scores. The honest answer is to log
				// nothing and leave the words in the leftovers, where the
				// preview points at the check-ins page.
				NoneOfKind(captured.KindCheckIn),
				UnparsedMentions("mood"),
			},
		},
		{
			ID:   "pounds",
			Why:  "Reading 172 lb as 172 kg is a number nobody would question and everybody would be wrong about.",
			Text: "weighed in at 172lb this morning",
			Draft: []DraftAssertion{
				WeightKG(78),
			},
		},
		{
			ID:   "words-not-numbers",
			Why:  "'Half a litre' is exactly the input a regex fast-path would miss, and the reason a model is here at all.",
			Text: "drank half a litre of water",
			Draft: []DraftAssertion{
				WaterBetween(400, 600),
			},
		},
		{
			ID:     "an-ambiguous-habit",
			Why:    "Two habits start with 'morning'. Picking one silently corrupts a streak the person cares about.",
			Text:   "did my morning thing",
			Habits: theirHabits,
			Prompt: []PromptAssertion{ListsTheirHabits()},
			Draft: []DraftAssertion{
				// Either answer is acceptable: naming one of the two lets the
				// service resolve it and show the choice, and leaving it
				// unparsed is equally honest. Naming a third thing is not.
				OnlyKnownHabits(),
			},
		},
		{
			ID:     "a-habit-they-do-not-keep",
			Why:    "Starting to keep a habit is a decision, not a side effect of mentioning it once.",
			Text:   "meditated for ten minutes",
			Habits: theirHabits,
			Draft: []DraftAssertion{
				OnlyKnownHabits(),
				UnparsedMentions("meditat"),
			},
		},
		{
			ID:     "a-workout",
			Why:    "Runs are not a capture kind. Logging one as something else puts a number in the wrong table.",
			Text:   "went for a 45 minute run",
			Habits: theirHabits,
			Draft: []DraftAssertion{
				// Most tempting as sleep, which is the other kind measured in
				// minutes.
				NoneOfKind(captured.KindSleep, captured.KindHabit, captured.KindWater),
				UnparsedMentions("run"),
			},
		},
		{
			ID:     "a-feeling-not-a-log",
			Why:    "This is a thought for the coach. Scoring it turns a transcription tool into one that judges.",
			Text:   "feeling rough about the thing at work",
			Habits: theirHabits,
			Draft: []DraftAssertion{
				LogsNothing(),
				UnparsedMentions("work"),
			},
		},
		{
			ID:     "a-log-and-an-errand",
			Why:    "The half it cannot read must survive. Swallowing it is what makes a log stop being trusted.",
			Text:   "2L water and I need to book a dentist",
			Habits: theirHabits,
			Draft: []DraftAssertion{
				Water(2000),
				Counts(1),
				UnparsedMentions("dentist"),
			},
		},
	}
}
