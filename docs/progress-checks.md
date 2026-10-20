# Progress checks

A progress check is front, side and (optionally) back photos plus weight and
tape measurements, taken on a cadence the person picks. It is a tracking tool,
not a medical one: no diagnosis, no judgement in the copy, estimates labelled
as estimates. Named "progress check" everywhere because "check-in" already
means the mood check-ins.

Code: `internal/progresschecks` (service, API, web handler),
`internal/progresschecks/progresscheck` (domain), `web/progress` (page).
API: `/api/v1/progress/*` and `/api/v1/body/current`, in `docs/api/openapi.yaml`.

## Photo estimates

"Estimate from photos" reads waist, hips and chest off the chosen photos and
fills the form. Nothing is stored by an estimate. The person saves what they
accept, and the check is stored with `source = assisted` plus the model's
confidence and note.

### Where photos go

In this order, and only one of them:

1. The person's own connected provider (`aicreds`), if it can read images.
2. Khepri's own Anthropic client (`registry.Get("anthropic")`).
3. Nowhere. The page shows the manual form only, and the API's
   `GET /progress/assist` answers `available: false`.

Photos never go to the free fallback chain, and an estimate never fails over
from one destination to the other. The page and `GET /progress/assist` name
the provider before anything is sent, so a silent switch would make that
statement untrue. If the named provider fails, the estimate fails.

### Decisions

- **Rate limit.** Quota action `progress_estimate`: 10 an hour on free, 30 on
  pro (`QUOTA_PROGRESS_ESTIMATES_PER_HOUR`,
  `QUOTA_PRO_PROGRESS_ESTIMATES_PER_HOUR`). Refused requests count too, the
  same as every other quota. Estimates paid by Khepri go through its Anthropic
  key, which during the Haiku trial is the `haiku-trial` Console workspace
  capped at $20 (see `platform/infra/docs/llm-haiku-trial.md`). Spend is
  recorded under surface `progress_estimate`.

- **"Can read images" is per provider, not per model.**
  `providers.BYOProvider.SeesImages` describes each catalogue provider's
  default model: true for OpenRouter, OpenAI, xAI, Gemini and Anthropic, false
  for NVIDIA and Hermes. A provider marked false is passed over for Khepri's
  client. A person who set a text-only model on a provider marked true gets an
  error ("enter your measurements by hand"). Their photos are not sent to
  Anthropic instead, for the reason above.

- **An earlier check is sent as scale.** The newest earlier check that has a
  tape-measured waist and a front or side photo goes along with its date and
  waist, plus height when known. A call therefore carries up to five images:
  today's front, side and back (back only when given), and the earlier front
  and side. Earlier back photos are never sent.

- **Note language.** The prompt (`internal/ai/prompts/progress_estimate.md`)
  is in English and contains no per-person data, so its prefix can be cached.
  The person's language is named in the user message ("Write the notes in
  European Portuguese (pt-PT)."), so the note shown under the numbers comes
  back in their app language. Everything else on the page comes from the i18n
  catalogue.

### Limits on the photos

JPEG, PNG or WebP, at most 3.5 MB each, checked by content rather than by file
name. Front or side is required: a back photo alone shows no waist. The web
page shrinks photos to 1600 px JPEG in the browser before upload, which also
drops EXIF and location. iOS should do the same.

### The model's answer

Every schema field is required, because OpenAI's strict mode refuses a schema
with optional fields. `0` means "not visible". `progresscheck.Estimate.Clean`
drops zeros and anything a save would refuse, so a misread photo comes back as
an empty field rather than a clamped number. Unknown confidence, or nothing
estimated, reads as `low`. No temperature, `top_p` or prefill is sent: Haiku
5.5 rejects them. A reply that is not the JSON asked for is retried once.

### Testing without a key

Tests use stub `ai.Client`s and never a real key. To run the real Anthropic
adapter locally against a stub server, set `ANTHROPIC_API_KEY` to any string
and `ANTHROPIC_BASE_URL` to the stub. The SDK honours that variable. The stub
answers `POST /v1/messages` with a Messages API body whose text is the
estimate JSON.

## Comparing first and latest

"Compare first and latest" sends the front and side photos of the person's
oldest check that has one and their newest, with each check's date and tape
measurements, and asks for a short neutral observation of visible change: by
area and pose, cautious where the photo setups differ, in the person's
language. Up to four images per call. It follows the same destination rules as
estimates and shares their quota (10 an hour free, 30 pro). Spend is recorded
under surface `progress_compare`.

- **Stored, one per person.** `progress_check_comparisons` keeps the latest
  comparison; comparing again replaces it. Both checks cascade, so deleting
  either deletes the observation: text derived from photos never outlives
  them. It is included in the data export.
- **Stale.** When a newer check with photos exists, the page and the API
  (`stale: true`) say so and offer to compare again. Nothing re-runs on its
  own, so nothing is spent without the person pressing the button.
- **The coach reads it as text.** `progresschecks.ContextSource` adds the
  observation to the coach's fitness summary, dated and labelled "a model's
  observation from photos, not a measurement". The coach never sees the
  photos. For a person on the free tier the coach runs on the free provider
  chain, so this text, unlike the photos, does reach those providers.
- **API.** `POST /progress/compare` makes one; `GET /progress/checks` carries
  `comparison` and `canCompare`.

## The figure takes its shape from the latest check

Today's body figure is shaped by the latest check's waist, hips and chest
(and BMI, softly). The server works out four weights, `bodymap.Shape`, from
the measurements over height against `bodymap.ShellBaseline`; the web page
renders them into the figure's props, and `/body/current` returns the same
four numbers for iOS, so the clients cannot disagree.

The mesh side lives in `scripts/bodymap` and `web/assets/models/README.md`:
four shape keys on the skin only, so the same muscles sit inside a wider
body. `ShellBaseline` is generated from the mesh by the same run that writes
the shape keys, in `internal/bodymap/baseline_gen.go`, so rebuilding the
figure can never leave the server's weights describing a different mesh.

Only the person's own figure is shaped. An exercise's figure, which shows
what a movement works, stays as modelled. iOS applies the same four weights
to the skin it builds from `BodyShell.bin` (see the models README).
