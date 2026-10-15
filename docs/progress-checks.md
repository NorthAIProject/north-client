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
