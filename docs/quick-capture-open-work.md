# Quick capture — what was left undone

Written 2026-09-04, the day quick capture landed (`internal/capture`, the five
logging capabilities in `internal/agent/logging.go`, and the JSON twin at
`/api/v1/capture`). This is a decision record and a starting point, not a build
order.

> **Updated 2026-09-04, later the same day.** Sections 1 and 2 are **done**, and
> the corpus is green against `openrouter/anthropic/claude-sonnet-4.5` — read
> the caveats at the end of section 1 before treating the parse as settled on
> every model. Section 3 is untouched and still gated. Running the evals also
> turned up a bug with nothing to do with capture: `meta/llama-3.3-70b-instruct`
> is dead and North still defaults to it, recorded in section 4.

It exists because the feature shipped with three known gaps, and a gap that is
only mentioned in a hand-off message stops existing the moment the message
scrolls away. Two of the three are deferred on purpose. One is not deferred —
it is a correctness hole that should close before strangers type into the box.

Read them in this order. They are listed by how much they matter, not by how
interesting they are.

---

## 1. The parse has never met a real model

**This is the one that is not deferred.** *(Done. Harness built, corpus graded
green — see the end of this section for what that does and does not prove.)*

Every test of the parser uses `internal/ai/fake` or a stub:
`internal/capture/parse_test.go` grades `convert` and `build` against
hand-written `modelItem` values, and `internal/capture/service_test.go` injects
a `stubParser`. Both are the right tests for what they cover — unit conversion,
range refusal, the coverage check, the commit fan-out — and none of them touches
`internal/ai/prompts/quick_capture.md`.

So the following are all currently unknown:

- whether a real model puts "2L" in `amount_ml` as 2000 rather than 2
- whether it obeys "do not guess the other half of a check-in" instead of
  inventing an energy score to go with a stated mood
- whether it honours the habit list it is given, or names a habit the person
  does not keep
- whether it reports what it could not read in `unparsed`, or silently drops it
- whether `weight_unit` comes back as the person's own unit or is quietly
  normalised to kg by the model before we get a chance to

That last group matters more than the first. The value conversions are graded by
`convert` and fail loudly. The instruction-following ones fail *silently*, as a
plausible number in a log nobody re-reads.

### The gate

There isn't one. Write these before the box is put in front of anybody who is
not you.

### What to build

The harness already exists and does not need designing: `internal/ai/eval` runs
one set of cases at two depths — offline with no provider on every push
(`grounding_test.go`), and live behind the `live` build tag against a real
provider (`grounding_live_test.go`, `eval.Provider(t)`, `task test:live`). Its
package doc explains why the two tiers share fixtures: evals that hand-write
their own version of what the application sends end up grading a format that no
longer exists. Capture wants exactly that arrangement.

What does **not** transfer is `eval.Case`. It is shaped around the coach —
`coach.Context`, `PromptBuilder`, `Reply` assertions over prose. A capture case
grades a *parse*: text in, typed items out. That is a different assertion
vocabulary and deserves its own case type rather than a `Context` field left
nil on every row.

Sketch:

```
internal/capture/eval/
    case.go        CaptureCase{ID, Why, Text, Habits, Want []Expect, WantUnparsed []string}
    cases.go       the corpus
    parse_test.go  offline: no provider — grades the rendered prompt only
    parse_live_test.go   //go:build live — grades what a model returned
```

The offline tier can still do real work without a provider: render
`prompts.QuickCapture` with a fixture and assert the habit list actually reaches
the prompt, that the local date is the person's and not the server's, and that
the "never invent an entry" line is present. That is the capture equivalent of
the grounding tier's "did the fact reach the model at all", and it catches the
regression that matters most cheaply — someone editing the prompt and dropping
the habit block.

The live tier asserts on the decoded `Draft`, not on text.

**Two mechanical things that will bite:**

- `task test:live` runs `go test -tags live -count=1 -v ./internal/ai/...`.
  Cases under `internal/capture/eval` are **not** in that path. Widen the task
  or the new tier will pass by never running — which is the same failure mode as
  a database test skipping silently.
- Grade a run as a *rate*, not as pass/fail per case. A model is not
  deterministic even at temperature 0, and a suite that goes red on one flaky
  row gets muted within a week. Run each case a few times and assert a
  threshold, with the failing outputs printed.

### Corpus worth having on day one

Every one of these is a sentence somebody will actually type:

| Text | What it must not do |
|---|---|
| `slept 6h, 2L water, read 20 pages, 78kg, mood 4 energy 3` | miss any of the five |
| `mood 4` | invent an energy score |
| `172lb` | return 172 kg |
| `half a litre of water` | fail, or return 0.5 |
| `did my morning thing` (two habits start with "morning") | pick one |
| `meditated` (no such habit) | invent a habit |
| `went for a 45 minute run` | log it as anything — runs are not a capture kind |
| `feeling rough about the thing at work` | log a check-in nobody asked for |
| `2L water and I need to book a dentist` | drop the dentist from `unparsed` |

The last two are the ones to watch. They are where a transcription model starts
behaving like a coach.

### Where this actually got to

**Built**, in `internal/capture/eval`:

- `case.go` — `Case`, `PromptAssertion`, `DraftAssertion`, and the constructors
  (`Water`, `Sleep`, `WeightKG`, `Feels`, `HabitNamed`, `FoodAbout`,
  `NoneOfKind`, `LogsNothing`, `Counts`, `UnparsedMentions`, `OnlyKnownHabits`),
  in the adapter-struct shape `internal/ai/eval` uses.
- `cases.go` — all nine cases from the table above.
- `render.go` — `RenderFor`, which both tiers call. `capture.RenderPrompt` was
  extracted from `AIParser.Parse` for this, so the evals grade the prompt
  production sends rather than a copy of it.
- `parse_test.go` — the offline tier. Green, runs on every push, no provider.
- `parse_live_test.go` — the live tier, `//go:build live`, running the real
  `AIParser` over a runner holding the live client.
- `Taskfile.yml` — `test:live` widened to `./internal/ai/... ./internal/capture/eval/...`,
  which was the trap named above.

**The grade, 2026-09-04, `openrouter/anthropic/claude-sonnet-4.5`:** all nine
cases pass, 3 of 3 runs each, 27 successful calls, 84 seconds.

That covers every behaviour the corpus grades — the five-item sentence, the
pound conversion, "half a litre", and, more importantly, all four refusals: it
does not invent the other half of a check-in, does not name a habit the person
does not keep, does not log a run as sleep, does not score a passing thought,
and does not swallow the dentist.

**What it does not prove**, and both are worth keeping in view:

- **Only one model has been graded, and it is a strong one.** The free tier does
  not run sonnet. A weaker model is a much harsher test of instruction
  following, and the refusal cases are exactly where a weak model gets tempted.
  Run the corpus against whatever the free chain actually resolves to before
  trusting it there. That is one command: `EVAL_PROVIDER=openrouter
  EVAL_MODEL=<model> task test:live`.
- **The correction retry from section 2 was never exercised.** A strict-schema
  provider rarely returns undecodable JSON, so those 27 calls all decoded first
  time. Section 2 remains covered by unit tests and unproven in the field.

Getting here also took two failed attempts, which is why section 4 exists: the
NVIDIA default is a dead model, and the one model that account could call was
slower than the two-minute per-call bound in `internal/ai/eval`.

### What running it changed about the harness

Two design corrections, both found by running the thing rather than by writing
it:

- **A refused call is not a failed case.** The first version counted a provider
  timeout as a run that failed the assertions, so a network problem read as
  "your prompt is wrong" — the fastest way to make a suite people ignore. The
  live tier now counts `graded` and `refused` separately, computes the rate over
  graded runs only, and skips a case that never reached the model, with the
  refusal count in the message.
- **But an entirely skipped suite must not report ok.** That is the same lie as
  a database test skipping when `TEST_DATABASE_URL` is unset. If no case
  anywhere was graded, the parent test now fails outright: somebody who typed
  `task test:live` wanted an answer, and a green tick over nothing is worse than
  a red one.

---

## 2. A malformed reply has no second chance

*(Built.)*

Found while writing the above, and worth separating because it is a code change
rather than a test.

`AIParser.Parse` returns a wrapped error when the reply does not decode:

```go
if decErr := json.Unmarshal([]byte(resp.Text), &candidate); decErr != nil {
    return apperr.Wrap(decErr, "the reply was not valid JSON for the required shape")
}
```

That error is not `ErrPaymentRequired`, `ErrUnavailable` or `ErrForbidden`, so
`ai.Failover` (`internal/ai/registry.go:113`) returns false and `Runner.Run`
**stops the walk immediately**. The person gets "Something went wrong reading
that. Try again." and their sentence back. One bad reply, one dead request.

`internal/workouts/service.go:155` does the other thing, and its comment says
why it works: the retry quotes the exact violation, because "try again" produces
the same answer and "you used a barbell and they only have dumbbells" does not.
It allows `generationAttempts = 2` per provider.

This matters unevenly, and the reason is in `internal/ai/eval/eval.go:49,51`:
`nvidia` and `hermes` are registered with `supportsJSONSchema: false`. For those
backends `openaicompat` drops `response_format` and moves the schema into the
prompt instead (`TestSchemaMovesIntoThePromptWhenStrictModeIsUnsupported`) — so
the shape is *asked for* rather than *enforced*, and a malformed reply stops
being a rare event. On a strict-schema provider this is nearly unreachable; on
Hermes it is a Tuesday.

**What to build:** the workouts loop, one attempt of it. On a decode failure,
append the model's own reply and a correction message naming the problem, and
ask once more before giving up on that provider. Do not raise it past two: a
capture is a cheap call a person is waiting on, not a training plan.

Do this **after** section 1, so the eval corpus can tell you whether it changed
anything.

### What was built

`parseAttempts = 2` in `internal/capture/parse.go`. On a decode failure the
parser appends the model's own reply plus a correction naming the problem and
asks once more, per provider, before the walk moves on — the workouts shape.
Four tests in `internal/capture/parser_retry_test.go` cover it: a malformed
reply is corrected, a persistently bad provider gives up after exactly two, a
good reply is never retried (the retry must not double the bill), and the schema
goes with the correction as well as the first ask.

The caveat from the sentence above still stands: the corpus has not yet told us
whether this changed anything in practice, because section 1 has not produced a
grade.

---

## 3. Voice

**Deferred, and the box was built so that this stays cheap.**

There is no audio anywhere in the repo today: no `MediaRecorder`, no
`getUserMedia`, no transcription client, nothing in `internal/ai` that takes a
waveform. `internal/media` handles *video* for form checks — multipart upload,
object storage in parts (`internal/media/storage.go`), and an async
`analyze_form_video` job (`internal/jobs/jobs.go:34`). That is the pipeline
voice would follow, not a new one.

### Why it was not built with the feature

Typing is the friction quick capture exists to remove, and voice removes more of
it than anything else — on a phone, talking beats typing decisively. That is a
real argument and it is why this document exists rather than a shrug.

It lost on sequencing, not on merit:

1. **Voice is a second input into the same pipe, not a second feature.** Speech
   becomes text, text goes to `Service.Parse`, and everything after that — the
   preview, the coverage check, the commit fan-out, the receipt — is already
   built and tested. Nothing about adding voice later invalidates any of it.
   That is the whole reason the parser takes a `string` rather than owning its
   own input.
2. **It doubles the surface before anyone has typed into the box once.**
   Transcription is a provider integration, a cost centre, a permission prompt,
   a recording UI, and a new failure mode ("it heard 'seventy' as 'seventeen'"),
   all in service of a parse whose accuracy is — see section 1 — currently
   unmeasured. Voice on top of an unverified parser is two unknowns multiplied.
3. **The cheap 80% already shipped.** The PWA share target
   (`web/pwa/manifest.webmanifest`) means dictating into the phone's own notes
   app and sharing it to Khepri already works, using the platform's
   transcription rather than one North pays for. That is worth watching before
   buying anything: if nobody shares into the box, nobody will hold a mic
   button either.

### The gate

Do not start until all three are true:

| Number | Read from | Threshold |
|---|---|---|
| The parse is trustworthy | the eval rate from section 1 | Instruction-following cases pass consistently, not just the unit conversions |
| The box is used | captures per active account per week | People are typing into it unprompted, not only from a nudge |
| Text is the bottleneck | share-target arrivals vs typed captures, and capture length | Shares are a real share of use, or typed captures are short enough to suggest people are giving up on long ones |

If the box is not used, voice is a faster road to a place nobody goes.

### What it would look like

Deliberately small, and deliberately not a conversation:

- **Client**: `MediaRecorder` behind a press-and-hold button next to the
  textarea, `audio/webm;codecs=opus`, hard-capped at ~60 seconds. Alpine for the
  button state only; the upload is a form post. No waveform, no playback UI, no
  pause. The one non-obvious requirement: `getUserMedia` needs a secure context,
  which the installed PWA has and `http://localhost` has, so local development
  is unaffected.
- **Server**: `POST /app/capture/voice` accepting one audio part under the same
  `MaxText`-shaped ceiling, transcribing it, and then rendering **the composer
  with the transcript in the textarea** — not the preview. The person reads what
  was heard before it is parsed. A mis-heard number that goes straight into a
  preview is a value nobody ever typed and nobody will notice.
- **Transcription**: behind an interface in `internal/ai` beside the existing
  clients, for the reason the whole AI layer is replaceable. One method,
  `Transcribe(ctx, io.Reader, mime string) (string, error)`. A new
  `spend.Surface`, and its own `quota.Action` — audio is the most expensive
  thing a person can hand North per second of their effort.
- **Storage**: none. Transcribe and discard. A voice note is not a document, the
  transcript is the artefact, and keeping raw audio of somebody saying "mood 2,
  argued with my partner" creates a retention question that buys nothing. If
  that is ever revisited, it is a settings toggle and a deletion path, not a
  default.

Synchronous, not a job. `analyze_form_video` is async because a video takes
minutes; a 20-second clip is a few seconds, and a person is standing there.

---

## 4. North defaults to a model that no longer exists

Not a capture problem. Found by running the evals, which is the sort of thing
evals are for even when the thing they find is not what they were aimed at.

`meta/llama-3.3-70b-instruct` reached end of life on 2026-08-26. NVIDIA now
answers every request for it with **410 Gone**, carrying that date in the body.

It is still North's default in four places:

| Where | What it affects |
|---|---|
| `internal/ai/providers/catalog.go:97` | the provider catalog's `DefaultModel` |
| `internal/config/config.go:492` | the `NVIDIA_MODEL` default |
| `internal/ai/eval/eval.go:49` | what `task test:live` evaluates against |
| `.env.example`, `docs/env-hosting.md` | what an operator copies |

So any deployment that enables NVIDIA without setting `NVIDIA_MODEL` gets a 410
on every call. The failover in `ai.Runner` softens it — 410 is wrapped as
unavailable, so the chain walks on to the next provider — which is exactly why
this could sit unnoticed: it degrades quietly into "NVIDIA never answers"
instead of failing loudly.

**Not fixed here, deliberately.** Choosing the replacement is a product decision
about cost and quality, and it needs a `internal/ai/pricing` entry to go with it
(the pricing table still lists the dead model). Two things worth knowing before
picking:

- The account this was tested on could call **only**
  `nvidia/nemotron-3.5-lightning-30b-a3b` out of the several tried;
  `nvidia/llama-3.1-nemotron-70b-instruct`, `nvidia/nemotron-nano-3-30b-a3b` and
  `mistralai/mistral-7b-instruct-v0.3` all answered 404 "not found for account".
  A model appearing in `GET /v1/models` does not mean it is provisioned.
- That model is slow enough to exceed the two-minute per-call bound in
  `internal/ai/eval`, which may matter for the chain's timeouts too.

---

## Anti-patterns to refuse

Each of these came up while writing this, and each is a way the above turns into
something worse.

- **Voice that goes straight to the preview**, skipping the transcript. The
  review step is the entire safety argument of this feature.
- **A conversation instead of a capture.** Holding the button should not open a
  dialogue with the coach — `/app/chat` exists and already streams. Capture
  transcribes and stops.
- **Keeping the audio** because it might be useful later.
- **Making the eval corpus pass by loosening it.** A case that keeps failing is
  information about the prompt, not about the case.
- **Grading the live tier pass/fail per case.** It will flake, and a flaky suite
  is a muted suite.
- **Raising the correction retry above two**, or adding a third provider hop for
  a decode failure. Somebody is waiting on this call.
- **A regex fast-path** in front of the model for "obvious" inputs. It handles
  "2L water" and misses "half a litre", which is precisely the input the feature
  exists for, and leaves two parsers to disagree.
