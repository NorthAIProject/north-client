# Quick capture — what was left undone

Written 2026-09-04, the day quick capture landed (`internal/capture`, the five
logging capabilities in `internal/agent/logging.go`, and the JSON twin at
`/api/v1/capture`). This is a decision record and a starting point, not a build
order.

> **Updated 2026-09-04, later the same day.** Sections 1 and 2 are **done**. The
> corpus is green on both a strong model (`anthropic/claude-sonnet-4.5`) and a
> free one (`nvidia/nemotron-3-ultra-550b-a55b:free`) — read the caveats at the
> end of section 1, which are now about rate limits and the untested retry
> rather than about the prompt. Section 3 is untouched and still gated. Running the evals also
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

**The free tier, same day, `openrouter/nvidia/nemotron-3-ultra-550b-a55b:free`:**
all nine cases pass. Eight in one run; `words-not-numbers` was refused there
("response contained no choices") and passed on a retry, so the nine have not
all passed in a *single* run, but every one has been graded green on a free
model. This is the more reassuring of the two results — the free chain is what
most people will actually hit, and the refusal cases are where a weaker model
gets tempted.

**What neither result proves:**

- **The correction retry from section 2 was never exercised.** Every call that
  decoded, decoded first time. Section 2 stays covered by unit tests and
  unproven in the field.
- **Free models are rate-limited hard enough to be their own obstacle.**
  OpenRouter caps free models at 20 calls per minute and the upstream pools have
  their own limits, so a 27-call run (nine cases × three) reliably 429s partway.
  Use `EVAL_RUNS=1` on a free model. `z-ai/glm-5.2:free`, the documented head of
  the free chain, was rate-limited throughout and never graded at all.

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
- **But a mostly skipped suite must not report ok.** That is the same lie as a
  database test skipping when `TEST_DATABASE_URL` is unset. The parent now fails
  unless at least half the cases it attempted actually reached the model —
  "attempted", not "exist", because `go test -run` narrowing to one case is
  legitimate and an earlier version of this guard failed every such run. Both
  halves were found by running it: the first version only caught a total
  blackout, and a run where eight of nine cases were rate-limited away still
  came back green.

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

**Built, 2026-09-06.** This section was a deferral with a three-part gate. The
gate was retired rather than met, and the reasoning below is kept so that
decision is legible rather than buried in a diff.

### What was gated, and why it was let go

The gate asked for three things before starting: a parse proven trustworthy by
the section 1 evals, evidence the box was used unprompted, and evidence that
typing was the bottleneck. The first is now true — 9/9 across three runs on
`anthropic/claude-sonnet-4.5`, 8/9 on the free chain. The other two were never
going to become true on their own, because nobody outside the project has used
North at all. Waiting for usage data from zero users is waiting for a number
that cannot arrive.

So the argument inverted. Voice was gated as supply-side polish — more surface
on a product nobody had tried. It is better read as the opposite: on a phone,
talking beats typing decisively, and a box you can talk into is the version of
this that is worth showing somebody. Voice is not what North adds once it has
users; it is part of why anyone would become one.

That is a judgement about demand, not a discovery about the code, and it should
be reversible. If people install North and never hold the button, the honest
reading is that this section was right the first time.

### What shipped

Deliberately small, and deliberately not a conversation.

- **Client**: `web/assets/js/shared/capture-recorder.js`, a delegated listener
  rather than an Alpine component so it survives every panel swap with no
  re-initialisation and cannot race Alpine's own deferred start. `MediaRecorder`
  in whatever container the browser offers, hard-capped at 60 seconds by a timer
  the person does not control. No waveform, no playback, no pause.

  **The clip is re-encoded to 16 kHz mono WAV before it is uploaded**, using
  `decodeAudioData` and an `OfflineAudioContext` — no dependency, since the
  browser already decodes every container it can record. This is not a nicety.
  `MediaRecorder` produces webm, ogg or mp4 depending on the browser, and the
  OpenAI dialect that every non-Gemini provider speaks names exactly two audio
  formats: `wav` and `mp3`. Uploading the browser's own container would make
  voice notes work on one provider and fail on the rest, which is the opposite
  of what a replaceable AI layer is for. A minute lands around 1.9 MB.

  Tap to start and tap to stop, not press-and-hold as this document originally
  sketched. Holding a button cannot be done from a keyboard, and a minute is
  long enough that holding a phone still is the worse gesture. The button is
  `hidden` until the script confirms `MediaRecorder` and `getUserMedia` exist,
  so an unsupported browser sees the typed box exactly as it was.

- **Server**: `POST /app/capture/voice`
  (`internal/capture/voice_handler.go`), guarded by its own
  `quota.VoiceCapture`. It renders **the composer with the transcript in the
  textarea**, never the preview. The person reads what was heard before it is
  parsed. A mis-heard number that goes straight into a preview is a value nobody
  typed and nobody will notice, and that review step is the whole safety
  argument of the feature.

- **The container is sniffed, never declared** (`internal/capture/voice.go`).
  `http.DetectContentType` answers `video/webm` and `video/mp4` for containers
  holding only an audio track — which is precisely what `MediaRecorder`
  produces — so taking its word would mean either refusing every real recording
  or widening an audio endpoint's allow-list to video. Five signatures are
  matched directly instead. The client's `Content-Type` is a claim; the bytes
  are the fact.

- **Transcription**: `ai.Transcriber` in `internal/ai/transcribe.go`, separate
  from `ai.Client` and type-asserted for, the same shape `ai.Embedder` has and
  for the same reason. The production implementation, `RunnerTranscriber`, asks
  a multimodal chat model through the ordinary provider chain: `ai.Part` already
  carries `InlineData` and a MIME type, Gemini ingests audio natively, and going
  through a registered client means the chain walk and the spend meter both
  apply with nothing new written. Zero new vendors, zero new credentials.

  Getting there needed one fix outside this feature.
  `internal/ai/openaicompat` rendered **every** inline part as `image_url` with
  a data URL, because until now every inline part was a photo. Audio sent that
  way reaches the provider as a picture: no error naming audio, just a model
  that saw nothing. It now emits `input_audio` for any `audio/` MIME, with the
  bare format word the dialect wants rather than a MIME type.

  The interface exists anyway, because the two ways to buy transcription are
  genuinely different products. A dedicated speech endpoint — `gpt-transcribe`
  at $0.0045/min, Deepgram Nova-3 at $0.0043/min, ElevenLabs Scribe v2 at
  $0.22/hr — is a different request shape at a different price with different
  accuracy. Swapping to one should be a new implementation of one method, not an
  edit to the handler.

- **Storage: none.** Transcribe and discard. A voice note is not a document, the
  transcript is the artefact, and keeping raw audio of somebody saying "mood 2,
  argued with my partner" creates a retention question that buys nothing. The
  temporary file `ParseMultipartForm` may spill is removed on the way out of the
  handler.

Synchronous, not a job. `analyze_form_video` is async because a video takes
minutes; a 20-second clip is a few seconds, and a person is standing there.

### Do we need ElevenLabs?

No. ElevenLabs sells voice *output* — synthesis quality and a hosted agent
pipeline. What quick capture needs is voice *input* becoming text, which is the
other end of the problem. It becomes a real question only if North ever wants a
branded coach voice reading replies aloud, and that belongs with speech-to-speech,
not here.

### What this does not close

- **Audio tokens are priced as text.** `internal/ai/pricing` is per-model, not
  per-modality, and Gemini bills audio input at its own rate. Voice spend is
  therefore recorded against the right surface and the right model but at the
  text rate, so the figure is directionally right and not exact. Worth fixing
  when the ledger next matters; not worth a modality dimension today.
- **Duration is not measured server-side.** The 60-second cap is enforced by the
  client and bounded on the server only by `MaxAudioBytes`. Counting frames
  would mean decoding the container, which is a codec dependency bought to
  re-check something a byte ceiling already bounds.
- **The chain's models are named for the wrong provider.** `.env` carries
  `AI_MODEL=gemini-2.5-pro` and `AI_FAST_MODEL=gemini-2.5-flash` alongside
  `AI_PROVIDER_CHAIN=openrouter,nvidia,fake` and an empty `GEMINI_API_KEY`.
  OpenRouter wants `google/gemini-2.5-flash`. Predates voice and affects the
  parse too; the local verification below was run with the slugs overridden.
- ~~**Coach chat still takes images only.**~~ Closed by the Telegram phase
  below, and half of it turned out to be the wrong problem. See "Voice on
  Telegram".

### Verified, 2026-09-06

Against the running app on a real provider, not a stub:

| What | Result |
|---|---|
| `internal/capture` suite with `TEST_DATABASE_URL` set | 35 pass, 0 skipped |
| macOS `say` WAV -> `POST /app/capture/voice` -> OpenRouter | `"Slept 6 hours, 2 liters of water, 78 kilos, mood four energy three."` in 2.2s |
| that transcript -> `/capture/parse` | sleep 360 min, water 2000 ml, weight 78 kg, check-in mood 4 energy 3 |
| the reply is the composer, not the preview | confirmed |
| a PNG posted as `audio/wav` | 422, "That did not arrive as a recording." |
| an empty part | 422, "That recording was empty." |
| no CSRF header or field | 403 |
| the button in a browser | visible after feature detection, still visible after an htmx panel swap, no recorder errors in the console |

Not verified by machine: holding the button. That needs a microphone and a
person, and it is the one step where `getUserMedia`, the re-encode and the
upload meet.

### The anti-patterns this refuses

- Voice that goes straight to the preview, skipping the transcript.
- A conversation instead of a capture. Holding the button must not open a
  dialogue with the coach; `/app/chat` already streams. Capture transcribes and
  stops.
- Keeping the audio because it might be useful later.

---

## 3b. Voice on Telegram

**Built, 2026-09-11.** Send the bot a voice note, get the answer you would have
got for typing the same sentence.

### The filter was not the blocker

The bullet above named two things: `hydrateCurrentTurn` filtering on
`part.Kind != "image"`, and the update struct decoding no `voice` field. Only
the second was real.

The filter matters if the audio is persisted and replayed to the model. Doing
that would have created exactly the retention question this section refused to
create, re-billed audio tokens on every later turn that re-inlined the part, and
made conversation history differ depending on which surface a message arrived
through. So the transcription happens in `messaging.Service`, which replaces the
recording with its words and clears the attachment. Everything downstream sees
an ordinary typed turn, and `internal/coach` was not touched.

It lives in the service rather than the adapter because what a recording costs,
how long is too long, and what to say when it cannot be heard are product
behaviour. In the adapter, the second platform copies them.

### What was actually in the way: Opus

A Telegram voice note is Opus in an Ogg container. `audioFormat` in
`internal/ai/openaicompat` names exactly `wav` and `mp3` — the two the dialect
carries — so Opus reaches every provider but Gemini as a format it refuses, and
the deployed chain is `openrouter,nvidia,fake` with an empty `GEMINI_API_KEY`.

It is worse than a refusal. `Client.File` sniffs a download with
`http.DetectContentType`, which answers `application/ogg` for this container.
That has no `audio/` prefix, so `openAIContent` would have taken the `image_url`
branch and sent a base64 waveform behind a data URL — the precise failure that
function's own comment says it was rewritten to prevent. Verified rather than
assumed: `http.DetectContentType` on a real voice note returns
`"application/ogg"`.

The web recorder never met any of this because the browser re-encodes to 16 kHz
mono WAV before uploading (see above). A phone has no browser to do that, so the
conversion moved to the server, and **ffmpeg is now a runtime dependency**. Both
surfaces go through `internal/voice` and `internal/shared/audio` so they cannot
drift: `audio.ChainSafe` is the rule written down, commented with the line in
`openaicompat` it is keyed to.

The alternatives were priced and rejected. Requiring Gemini makes one vendor
mandatory for a feature and would have shipped dead against the current config.
A dedicated speech endpoint (`gpt-transcribe` $0.0045/min, Deepgram Nova-3
$0.0043/min, Scribe v2 $0.22/hr) takes Opus directly and is the better eventual
answer — `ai.Transcriber` exists so it is one implementation, not an edit here —
but it is a new vendor, a new credential, and a per-minute bill. ffmpeg is a
package in the image that costs nothing per call.

This is the first `os/exec` in the tree, in the web pod's request path. Fixed
argv, never a shell, stdin to stdout with no temp file and no path derived from
anything a person sent, no `-f` guessed from a declared type, bounded output,
bounded stderr, a 30 s timeout, a `WaitDelay`, and a semaphore of four so a
burst of voice notes cannot fork-bomb a pod. Resolved once at boot: without
ffmpeg the app starts, typed paths work, the web recorder works, and a
compressed recording is refused in words rather than forwarded as bytes no model
can read.

### Bounds, budgets and what is said

Four minutes, and the number is arithmetic: decoded to 16 kHz mono 16-bit PCM,
speech is 32 KB/s, so the 8 MiB ceiling is 262 seconds. Confirmed in practice —
a 14 KB Opus note converts to 147 KB of WAV.

Duration and size are both checked **before** the quota, so a recording refused
for its length does not also cost a dictation. Voice spends the same
`quota.VoiceCapture` budget as the web recorder, because it is the same act and
a per-person allowance should not double because somebody opened a different
app. It gets its own spend surface, `telegram_voice`, because the ledger should
be able to say which product the audio money went to.

Transcription failures are answered, never returned: an error returned from here
reaches the bridge as the generic apology, which is true and useless to somebody
who just spoke into their phone. Silence asks again rather than handing the
coach an empty turn to invent a subject for.

Only the `voice` field is decoded. Not `audio` — a forwarded album would buy an
hour of transcription with one tap — and not `video_note`.

### What this does not close

- **Nothing catches a mis-heard fact.** The transcript is not echoed: the reply
  is the reply. Mis-heard *writes* are still caught, and better than on the web
  — the `PendingApproval` gate spells the values back before anything is
  persisted, which shows the number about to be written rather than the sentence
  that produced it. But "I did not train" heard as "I did train" changes the
  coaching and nothing sees it. Accepted, not overlooked. The hedge is two lines
  in `transcribeVoice`, deliberately.
- **Spoken replies are not built.** The blocker was never ffmpeg: OpenRouter
  does not serve `/audio/speech` and neither does NVIDIA, so voice-out means a
  second vendor, a second credential, and a provider with no failover. The seam
  is in `internal/voice` so it stays additive.
- **Audio is still priced as text**, as above. This adds a second surface to
  that inaccuracy without changing its shape.
- **The model slugs are still named for the wrong provider**, as above. The
  verification below was run with `AI_FAST_MODEL` overridden, for the same
  reason the 2026-09-06 run was.

### Verified, 2026-09-11

| What | Result |
|---|---|
| `internal/shared/audio`, real ffmpeg: Ogg/Opus -> WAV | 16000 Hz, 1 channel, 16-bit |
| the WAV it writes to a pipe, read back by ffmpeg | valid; the unknown RIFF size field is tolerated |
| `internal/voice`, `internal/messaging`, `internal/capture` suites | pass, messaging against real Postgres |
| whole tree, `go test ./...` | 115 packages, 0 failures |
| `main voice-check` | ogg 3987 B -> wav 32078 B, chain-safe |
| macOS `say` -> Opus -> `voice-check --file --transcribe` -> OpenRouter | `"I slept 6 hours last night, drank 2L of water, and my mood is a four."` |
| `http.DetectContentType` on that voice note | `"application/ogg"` — the reason the declared type is kept as a hint and the bytes are sniffed |

Not verified by machine: a real voice note from a real phone to a real bot. That
needs a bot token and a person holding a microphone, and it is the one step
where Telegram's own encoding, the download and the conversion meet.

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

## 5. What running the app in a browser found

The plan's verification list ended with "run the app and check by hand". That
step was skipped when the feature shipped — everything was verified at the HTTP
level — and doing it afterwards found two things the tests could not.

**The Save counter was broken.** The preview's Alpine block counts ticked rows
for the Save label. It read the right number on load and then fell to **zero**
on the first tick of any checkbox:

```
recount() { this.count = this.$el.querySelectorAll(...).length }   // wrong
recount() { this.count = this.$root.querySelectorAll(...).length } // right
```

Inside a method reached from a checkbox's `@change`, Alpine's `$el` is the
element evaluating the expression — the checkbox — not the element holding
`x-data`. So the query ran against a node with no children. `init()` worked
because there `$el` really is the form, which is why it looked correct until
somebody touched it. `$root` is the component's root. Fixed, with the reason in
a comment above the template so it does not come back, and verified in the
browser: 3 → 2 → 1 → 2 across unchecking and rechecking.

No HTTP test could have caught this. The form posts exactly the same fields
either way; only the label was wrong.

**A sentinel was leaking into user-facing copy.** The receipt read:

> No measurement to update; record height, date of birth and sex once first**:
> validation failed**

`apperr.Wrap` composes `"%s: %w"`, which is right for a log and wrong for a
screen. `capture.Sentence` now strips the sentinel texts, capitalises, and
punctuates; `userFacing`, the handler and the API all go through it. There is a
test asserting the *property* — that nothing a person reads ends in one of
apperr's sentinels — rather than a list of examples.

**One thing left alone, worth knowing:** on the free chain a parse takes 30-60
seconds (the free models are slow and the first one in the chain was
rate-limited, so it fails over), and the only feedback is the Save button going
grey via `hx-disabled-elt`. The plan mentioned an `hx-indicator` and it was
never added. On a paid model the wait is 2-4 seconds and it does not matter; on
the free tier it reads as a dead button. Worth a skeleton or a spinner before
anybody uses this on the free tier.

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
