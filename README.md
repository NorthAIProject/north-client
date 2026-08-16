# North

<p align="center">
  <strong>Your AI Operating System for Personal Growth.</strong>
</p>

<p align="center">
  An intelligent coach that understands your goals, remembers your journey, and helps you make meaningful progress—wherever you are.
</p>

---

## Vision

Most AI assistants answer questions.

North helps you build a better life.

Instead of starting every conversation from scratch, North develops a long-term understanding of who you are, what you're working towards, and how your life evolves over time.

Whether you're planning a new project, training for a marathon, improving your health, learning a new skill, or simply trying to become more consistent, North acts as your personal coach—not just another chatbot.

---

## Core Principles

North is built around four ideas:

### 🧠 Persistent Memory

North remembers what matters.

- Goals
- Preferences
- Coaching style
- Conversations
- Check-ins
- Documents
- Health summaries
- Progress over time

---

### 🎯 Context-Aware Coaching

Advice should be based on your real life.

North builds context from:

- Active goals
- Recent conversations
- Calendar events
- Fitness activity
- Notes
- Uploaded documents
- Previous reflections
- Connected MCP tools

---

### 🌍 Available Everywhere

Your coach shouldn't live inside one application.

North is designed to be available through multiple interfaces while sharing one continuous memory.

Current & planned interfaces:

- 🌐 Web Application
- 💬 Telegram
- 💬 Discord
- 💬 WhatsApp
- 🧩 Claude Desktop (MCP)
- 📱 Native Mobile Companion (future)

---

### 🔓 AI Provider Agnostic

North is not tied to a single AI provider.

Supported providers include:

- OpenAI
- Anthropic
- Google Gemini
- xAI

The application depends on an internal abstraction layer, making providers replaceable without changing business logic.

---

# Features

## AI Coaching

- Persistent conversations
- Long-term memory
- Goal-oriented coaching
- Reflection sessions
- Decision support
- Brainstorming
- Accountability

---

## Goals

- Long-term goals
- Milestones
- Deadlines
- Progress tracking
- Success metrics

---

## Daily Check-ins

Structured reflections help North understand your progress.

Examples:

- Mood
- Energy
- Wins
- Challenges
- Notes

---

## Reports

Automatically generated:

- Daily Briefings
- Weekly Reviews
- Monthly Progress Reports
- Goal Summaries

---

## Knowledge Base

North understands more than chat.

Users can upload:

- PDF documents
- Images
- Videos
- Audio
- Voice notes
- Markdown
- Research papers

The AI can search, summarize and reference these documents when relevant.

---

## Fitness

North is designed to become your AI fitness coach.

Supported:

- Strava

Planned:

- Apple Health
- Google Health Connect
- Garmin
- Fitbit
- Polar

North uses summarized activity data to provide intelligent coaching.

---

## Integrations

North is built around MCP.

Examples:

- Calendar
- Notes
- Email
- GitHub
- Task Managers
- Custom MCP Servers

North also exposes its own MCP server so external AI assistants can interact with your coach.

---

## Multi-platform Messaging

North is designed to be accessible through:

- Web
- Telegram
- Discord
- WhatsApp

Every interface shares the same memory and coaching engine.

Telegram is implemented today. A message you send from your phone reaches the
same coach the web chat does, **in the same conversation** — ask something in
Telegram and the answer is in the web thread when you open it, and the other way
round.

See [Setting up Telegram](#setting-up-telegram) below.

---

# Architecture

```
                     Users

        Web
        Telegram
        Discord
        WhatsApp
        Claude Desktop (MCP)

                │
                ▼

          Go Web Application

                │

        ┌───────────────────┐
        │   Coach Service    │
        └───────────────────┘

                │

         Context Builder

                │

 ┌────────┬────────┬────────┬────────┐
 │ Goals  │Memory  │Fitness │MCP     │
 │Docs    │Reports │Calendar│Notes   │
 └────────┴────────┴────────┴────────┘

                │

           AI Provider

                │

          Streaming (SSE)

                │

          PostgreSQL
```

---

# Tech Stack

## Backend

- Go
- Chi
- Templ
- PostgreSQL
- pgx
- sqlc

## Frontend

- Server Side Rendering
- Tailwind CSS
- Alpine.js
- HTMX
- Server-Sent Events

## AI

- OpenAI
- Anthropic
- Gemini
- xAI

## Infrastructure

- Docker
- Kubernetes (k3s)
- Helm
- ArgoCD
- Terraform
- GitHub Actions

---

# Repository Structure

North is organised in **vertical slices**. Each feature owns its handler, service,
repository, and SQL in one directory, so a change to goals touches one folder rather
than four. Layering is enforced *inside* the slice, not across the tree.

```
cmd/
    web/            HTTP server
    worker/         background jobs
    mcp-server/     MCP server

internal/

    ai/             AIClient interface, provider registry, prompts
        gemini/
        openrouter/
        fake/
        prompts/
    auth/           sessions, passwords, RequireAuth
    users/
    coach/          CoachService, ContextBuilder, PromptBuilder
    conversations/
    checkins/
    goals/
    workouts/
    reports/
    fitness/        Strava and other providers
    documents/
    media/          object storage
    search/
    integrations/   MCP client
    messaging/      Telegram, Discord, WhatsApp adapters
    config/

    shared/
        database/   pgxpool
        errors/     sentinel errors
        middleware/ request id, logging, recover, CSRF
        types/

web/
    shared/
        layout/     base and app layouts
        ui/         templUI components
        utils/
    auth/           login, signup pages
    chat/
    workouts/
    landing/
    assets/         css, js, fonts (embedded)

migrations/         goose migrations
terraform/
charts/
docs/
```

Every slice follows the same shape:

```
internal/<feature>/
    handler.go      parse, validate, authenticate, call service, render
    service.go      business logic; depends on repository interfaces
    repository.go   thin wrapper over generated code
    db/queries.sql  sqlc input
    db/*.go         sqlc output (generated, committed)
```

Repositories never call services. Services never import handlers.


---

# Testing

```bash
task test        # the whole offline suite
task test:eval   # grounding evals only, offline tier
task test:live   # evals against a real AI provider (costs money)
```

`task test` needs nothing. Database-backed tests skip themselves when
`TEST_DATABASE_URL` is unset — worth knowing, because a suite that skips looks
a lot like a suite that passes.

## AI grounding evals

North's usefulness rests on the coach not inventing facts, so that claim gets
its own suite in `internal/ai/eval`. One set of cases, defined once in
`fixtures.go`, graded at two depths:

| Tier | Question | Cost | Runs |
| --- | --- | --- | --- |
| Offline | Did the facts reach the model? | free | every push, in CI |
| Live | What did the model do with them? | API spend | manually, before a release |

Both tiers render their fixtures through the real `coach.PromptBuilder`, so the
evals cannot drift into grading a prompt format the application stopped
sending.

Cases today: `goals-reach-the-prompt`, `no-invented-checkins`,
`citations-when-docs-exist`, `memory-respect`, `admits-what-it-was-not-told`.

### Running the live tier

Live evals sit behind the `live` build tag and are excluded from CI on purpose:
a model has bad afternoons, and an unrelated pull request should not go red
because a provider was slow.

```bash
export GEMINI_API_KEY=...        # or the key for whichever provider below
task test:live
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `EVAL_PROVIDER` | `gemini` | `gemini`, `openrouter`, `nvidia`, `xai`, `hermes` |
| `EVAL_MODEL` | per provider | Override the model under test |

Each provider reads its own key — `GEMINI_API_KEY`, `OPENROUTER_API_KEY`,
`NVIDIA_API_KEY`, `XAI_API_KEY`, `HERMES_API_KEY` — and skips rather than fails
when the key is absent, so a contributor with one key can run the evals they
can afford. `HERMES_BASE_URL` has no default and must be set to run that one.

A failure names the case, the assertion, the provider and model, why the case
exists, and the full reply that was graded:

```
--- FAIL: TestGroundingLive/citations-when-docs-exist
    case citations-when-docs-exist [gemini]: assertion CitesOnlyOfferedRefs:
    the reply cited "chunk:invented-42", which was never offered;
    offered were "chunk:physio-deload-1", "chunk:physio-knee-2"
```

### Adding a case

Add one entry to `Cases()` in `internal/ai/eval/fixtures.go` with a `Context`
fixture, an `Ask`, and both kinds of assertion. `TestSuiteCoversTheGrowthOSScenarios`
fails a case that is missing either kind, so a scenario cannot quietly stop
being evaluated.

---

# Design Philosophy

North is intentionally built as a **Server-Side Rendered** application.

Reasons:

- Fast initial load
- Excellent SEO
- Simple architecture
- Minimal JavaScript
- Easier maintenance
- Great developer experience
- Progressive enhancement

Client-side JavaScript is used only where it provides clear value.

---

# Guiding Principles

- Keep the architecture simple.
- Prefer explicit code over magic.
- Build features before abstractions.
- Business logic belongs in services.
- The AI provider is an implementation detail.
- Keep Go code idiomatic.
- Use HTMX before writing JavaScript.
- Avoid unnecessary dependencies.

---

# Roadmap

## Phase 1

- Authentication
- Goals
- Conversations
- Memory
- Check-ins
- AI Coaching

## Phase 2

- Reports
- Document uploads
- Semantic search
- MCP Client
- MCP Server

## Phase 3

- Telegram
- Discord
- WhatsApp
- Strava
- Calendar

## Phase 4

- Native iOS Companion
- Apple Health
- Widgets
- Live Activities
- Apple Watch

---

# Deployment

North is designed to run on Kubernetes.

Production stack:

- DigitalOcean
- k3s
- Helm
- ArgoCD
- Terraform
- GitHub Actions

Development can be run using Docker Compose.

---

# Setting up Telegram

> Telegram is one of North's gateways — the ways in that are not a browser.
> `docs/gateways.md` describes all of them side by side, including how far each
> one has actually been verified. This section is the setup walkthrough.

Telegram is optional. Without `TELEGRAM_BOT_TOKEN` the adapter is not built, no
route is mounted, and the card does not appear in Settings — everything else
works exactly as before.

Setup is in two halves: **once per deployment** (someone creates the bot) and
**once per person** (each person links their own chat).

## Part 1 — create the bot (once, by whoever runs North)

### 1. Create it with @BotFather

Open Telegram and message [@BotFather](https://t.me/BotFather):

```
/newbot
```

It asks two questions:

- **Name** — what people see at the top of the chat. Anything: `North`.
- **Username** — must be unique across all of Telegram and must end in `bot`.
  For example `north_coach_bot`.

It replies with a token that looks like this:

```
8154392017:AAH9k2LmQ7xVbN3pR8sTuW1yZ4cE6gI0jKm
```

**That token is a credential.** Anyone holding it controls the bot completely.
Treat it like a password: never commit it, never paste it into an issue.

### 2. Optional — turn off group access

North already refuses group chats: a group has one chat id shared by everybody
in it, so a linked group would let every member read the owner's goals and log
check-ins as them. If the bot is added to one it says so and leaves.

Turning the setting off as well just means it is never added in the first place.
Still in @BotFather:

```
/setjoingroups
```

Pick your bot, then **Disable**.

### 3. Put it in your `.env`

```bash
TELEGRAM_BOT_TOKEN=8154392017:AAH9k2LmQ7xVbN3pR8sTuW1yZ4cE6gI0jKm
TELEGRAM_BOT_USERNAME=north_coach_bot
```

`TELEGRAM_BOT_USERNAME` is only so the Settings page can tell people which bot
to open. Nothing authenticates against it.

### 4. Choose how updates arrive

Two modes, and **which one runs follows from whether you set a secret** — there
is no third variable and no combination that leaves an endpoint unprotected.

**Long polling — leave `TELEGRAM_WEBHOOK_SECRET` empty.**

North asks Telegram for updates over an outbound connection. Nothing is exposed,
no public URL is needed, and it works on `localhost`. This is the right choice
for development and fine for a single production instance.

```bash
TELEGRAM_WEBHOOK_SECRET=
```

Nothing else to do. Start North and the poller starts with it:

```
INFO telegram poller started
```

> Only one process may poll a given bot. Two pollers on one token each receive
> half the updates, so do not run this mode on more than one replica.

**Webhook — set `TELEGRAM_WEBHOOK_SECRET`.**

Telegram POSTs to `{BASE_URL}/webhooks/telegram`. This needs a **public HTTPS
URL**, so it is a production choice; it will not work against `localhost`
without a tunnel.

Generate a secret and register the webhook once:

```bash
# 1. Generate
openssl rand -hex 32

# 2. Put it in .env as TELEGRAM_WEBHOOK_SECRET, then deploy.

# 3. Tell Telegram where to send updates.
curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
  -d url="$BASE_URL/webhooks/telegram" \
  -d secret_token="$TELEGRAM_WEBHOOK_SECRET"
```

The secret is echoed back by Telegram in the `X-Telegram-Bot-Api-Secret-Token`
header and is the only thing separating a real delivery from anyone who guesses
the path. North answers `401` without it, which is why the secret is required
rather than optional in this mode.

Check what Telegram thinks:

```bash
curl "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getWebhookInfo"
```

`pending_update_count` climbing or a non-empty `last_error_message` means
deliveries are failing.

To switch back to polling, delete the webhook and clear the secret — Telegram
refuses to serve `getUpdates` while a webhook is registered:

```bash
curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/deleteWebhook"
```

## Part 2 — link your account (once per person)

Each person does this for themselves. There is nothing to configure.

1. Open North in a browser and sign in.
2. Go to **Settings → Agent connections**.
3. Find the **Telegram** card and press **Get a link code**.
4. A short code appears — something like `K7PQ2MNX`. It is shown **once** and is
   valid for **15 minutes**.
5. Open the bot in Telegram (the card links to it) and send it that code.
6. It replies confirming which account it linked.

That is the whole flow. From then on, just talk to it.

**Why a code at all.** A message arrives carrying a chat id and nothing else,
and that has to be turned into an account somehow. Until a chat is linked it can
do exactly one thing — redeem a code — so it never reaches the coach and nobody
who merely finds your bot can spend your model budget.

Some details worth knowing:

- Codes are stored hashed, so nobody can read one back out of the database. Lost
  it? Press the button again; issuing a new code invalidates the old one.
- A code works once. Sending it from a second chat does nothing.
- A chat already linked to a **different** account is refused rather than moved.
- Case and spacing are forgiven, so `k7pq 2mnx` works. The alphabet deliberately
  excludes `0`, `O`, `1`, `I` and `L`, so there is nothing to misread.

## Using it

Ask anything you would ask in the web chat:

> **You:** how did my week go?
>
> **North:** Three workouts and two check-ins, both positive. That is one more
> session than last week…

**When the coach wants to write something** — log a check-in, create a goal — it
asks first, exactly as the web app does:

> **North:** Before I do this, can you confirm?
>
> • create check in `{"mood":4,"note":"good day"}`
>
> `[ Yes, do it ]` `[ No ]`

Tap a button, or just type it — "yes", "no thanks", "go ahead" all work. Anything
ambiguous gets you the question again rather than a guess, because the wrong
guess writes something you did not agree to.

**Commands**, which your Telegram client also offers from the menu button:

| Command | Does |
|---|---|
| `/start` | Confirms which account this chat is linked to |
| `/help` | What it can do, and that writes are confirmed first |
| `/unlink` | Disconnects this chat. Nothing you have said is deleted |

None of them reaches a model, so none of them costs a message. Anything else
starting with `/` is treated as an ordinary question — "/summarise my week" is a
sentence, not a broken command.

Coach messages from Telegram count against **the same hourly quota** as the web
chat. Answering a confirmation is free: you asked one question, and saying yes to
it is not a second one.

## Troubleshooting

| Symptom | Cause |
|---|---|
| Bot never replies | Polling mode not running (`telegram poller started` missing from the logs), or a webhook is registered and blocking `getUpdates`. |
| "I do not know you yet…" | That chat is not linked. Get a code from Settings. |
| Bot leaves a group immediately | Working as intended. A group's chat id is shared by everyone in it, so North only works in a direct message. |
| Code rejected | Expired (15 min), already used, or superseded by a newer one. Issue another. |
| "already linked to another North account" | That chat belongs to a different account. Disconnect it there first. |
| Replies stop after a restart | A reply generated during a restart loses its push. The answer is still saved — open the web thread and it is there. |
| Webhook returns 401 | `TELEGRAM_WEBHOOK_SECRET` and the `secret_token` given to `setWebhook` disagree. Re-run `setWebhook`. |

## Disconnecting

**Settings → Agent connections → Telegram → Disconnect.** The bot stops
answering that chat immediately. Nothing you have said is deleted — the whole
conversation stays in the web app, because a messaging link is a way to reach
your account, never a way to sign in to it.

---

# Long-Term Vision

North isn't another AI chatbot.

It's a long-term thinking partner.

Over time it develops a deep understanding of:

- who you are,
- what you're building,
- what motivates you,
- how your habits evolve,
- where you're making progress,
- and where you need support.

The goal isn't to replace human judgment.

The goal is to augment it.

---

## Contributing

Before contributing, please read:

- `CLAUDE.md`
- `AGENTS.md`
- `ARCHITECTURE.md`

These documents define the project's architecture, coding standards, and AI-agent guidelines.

---

## License

MIT