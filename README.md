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