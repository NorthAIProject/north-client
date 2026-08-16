# ARCHITECTURE.md

> This document describes the architectural vision of North.
>
> It explains **how the system is designed**, **why decisions were made**, and **how new features should be integrated**.
>
> This document is intended for both human contributors and AI coding agents.

---

# Vision

North is an AI Operating System for Personal Growth.

Unlike traditional AI assistants that answer isolated questions, North maintains long-term context about a user's life, goals, habits, knowledge, and progress.

North should become a persistent thinking partner that exists across multiple interfaces while sharing one consistent memory.

---

# Architectural Principles

The architecture follows a few fundamental principles.

## Server First

North is built as a Server-Side Rendered application.

Reasons:

- Fast initial page loads
- Excellent SEO
- Minimal JavaScript
- Simpler architecture
- Easier maintenance
- Better accessibility
- Easier debugging

Client-side rendering should only be introduced when it provides a measurable benefit.

---

## Progressive Enhancement

The stack is intentionally simple.

```
Go

↓

Templ

↓

Tailwind

↓

HTMX

↓

Alpine

↓

Minimal JavaScript
```

JavaScript is not the application.

The server is.

---

## Thin Interfaces

Every interface is simply another way of talking to the same application.

Examples:

- Web
- Telegram
- Discord
- WhatsApp
- MCP
- Native Mobile (future)

No interface owns business logic.

---

## AI is Infrastructure

The AI model is replaceable.

North owns the intelligence.

The LLM provides reasoning.

Never build business logic around provider-specific features.

---

# High-Level Architecture

```
                           Interfaces

       Web
       Telegram
       Discord
       WhatsApp
       MCP
       Native (future)

                 │

                 ▼

          Presentation Layer

                 │

                 ▼

          Application Layer

                 │

                 ▼

          Domain Services

                 │

                 ▼

       Infrastructure Layer

     PostgreSQL
     AI Providers
     MCP
     Object Storage
```

---

# Layer Responsibilities

## Presentation

Responsible for:

- HTTP
- Templates
- HTMX
- SSE
- Authentication
- Validation

Never contains business rules.

---

## Services

The heart of North.

Contains:

- coaching
- context building
- reports
- memory
- goals
- check-ins

Every feature should eventually flow through services.

---

## Domain

Represents business concepts.

Examples:

- User
- Goal
- Conversation
- Report
- CheckIn
- Memory

The domain is independent from infrastructure.

---

## Infrastructure

Responsible for communicating with external systems.

Examples:

- PostgreSQL
- AI Providers
- Object Storage
- MCP
- Messaging APIs
- Fitness APIs

---

# Request Lifecycle

```
User

↓

HTTP

↓

Handler

↓

CoachService

↓

ContextBuilder

↓

PromptBuilder

↓

AI Client

↓

Streaming Response

↓

Save Conversation

↓

Return Response
```

Handlers never communicate directly with AI providers.

---

# CoachService

The CoachService is the core of North.

Responsibilities:

- Build context
- Execute tools
- Build prompts
- Call AI
- Store conversations
- Update memory

Everything conversational should pass through CoachService.

---

# Context Builder

North's intelligence comes from context.

The ContextBuilder gathers relevant information before every AI request.

Sources include:

```
User

Goals

Recent Conversations

Check-ins

Reports

Calendar

Fitness

Documents

Media

Knowledge Base

MCP Tools
```

The LLM receives a summarized view instead of raw database records.

---

# Prompt Builder

Prompt construction is centralized.

Responsibilities:

- System prompts
- Coaching style
- Tool descriptions
- User context
- Memory summaries

Prompts should never be embedded throughout the application.

---

# Memory

Memory is a first-class feature.

Memory is divided into:

## Short-Term

Recent conversation history.

Used for immediate context.

---

## Long-Term

Persistent facts.

Examples:

- preferred coaching style
- recurring habits
- personal interests
- long-term goals

---

## Semantic Memory

Knowledge retrieved using embeddings.

Includes:

- documents
- notes
- uploaded media
- journals

---

# Knowledge System

North treats uploaded content as knowledge.

Supported formats:

- PDF
- Images
- Video
- Audio
- Markdown
- Text

Pipeline:

```
Upload

↓

Storage

↓

Extraction

↓

Embeddings

↓

Search

↓

Context Builder
```

Never send complete documents to the LLM unless required.

---

# Fitness

North integrates with fitness providers.

Current:

- Strava

Future:

- Apple Health
- Health Connect
- Garmin
- Fitbit

Fitness providers produce summarized insights.

CoachService consumes summaries—not raw activities.

---

# MCP

North has two roles.

## MCP Client

Connects to:

- Calendar
- Notes
- GitHub
- Tasks
- Email

---

## MCP Server

Exposes:

- Goals
- Reports
- Knowledge Search
- Check-ins
- Coach

The MCP server should expose business capabilities—not implementation details.

---

# Messaging

Messaging platforms are treated as presentation layers.

```
Telegram

↓

Adapter

↓

CoachService
```

```
Discord

↓

Adapter

↓

CoachService
```

Every messaging platform should reuse the same application logic.

Messaging platforms are one kind of *gateway* — a way into North that is not a
browser, alongside MCP and health ingest. `docs/gateways.md` is the register of
all of them: how each authenticates, and how far each has been verified against
the real service on the other side.

`internal/messaging` is the adapter. A platform stops at `InboundMessage` on the
way in and `Transport` on the way out; nothing downstream knows Telegram exists.
The adapter builds no prompt and chooses no provider — it resolves who is
speaking, meters the turn, finds the thread, and calls `SendMessage`.

## Identity

A message arrives carrying a chat id and nothing else. Turning that into an
account is the whole problem, and North already had three shapes for it:

| Table | Maps | Used for |
|---|---|---|
| `auth_identities` | `(provider, subject)` → user | Signing in with Google |
| `strava_connections` | user → provider data, one way | A data source |
| `agent_connections` | hashed bearer → user | MCP and health ingest |

`messaging_links` takes `auth_identities`' shape, because the question is the
same one. It is a **separate table** for the reason `strava_connections` is:
messaging is not a way to sign in. If it were, unlinking Telegram could lock
somebody out of their account — a steep price, and a chat id is a far weaker
credential than one that should be able to do that.

The binding is made with a one-time code:

1. The web app, where the person is already authenticated, issues a code. Only
   its SHA-256 hash is stored, and issuing a new one invalidates the last.
2. They send it to the bot.
3. Redemption is atomic and single use, rate limited per chat, and refuses a
   chat already bound to a different account rather than moving it.

Sending that code is the **only** thing an unlinked chat can do. It never
reaches the coach, so finding the bot buys a stranger nothing — there is no
account to charge a model call to.

**Private chats only.** A link binds an account to one conversation id, and on
every platform a group has a single id shared by everybody in it. Linking one
would hand every member the owner's goals, check-ins and write approvals, so the
adapter refuses any chat that is not one person and removes itself rather than
sitting in a chat whose id could be linked later. The messaging layer refuses
the same id again on the way in — the platform adapter knows what a group is,
and the backstop only needs to know that its id is negative.

## One thread

A linked person has one conversation, not one per surface. The adapter
continues the newest live chat thread, which is the thread the web app is
showing. Asking on the phone and asking in the browser continue each other.

The alternative — a conversation per platform — would leave the coach with two
half-histories and make it worse at the one thing it exists to be good at.

## Delivery

Adapters acknowledge an update **before** they answer it. A coach turn can take
minutes and every platform retries anything slower than a few seconds, so the
reply is generated on a detached context and pushed when ready.

The cost is that a restart mid-generation loses the push. The reply is still
persisted and still appears in the web thread, so what is lost is the
notification rather than the answer. Redeliveries are made harmless by the
`last_update_id` watermark on `messaging_links`.

Which edge receives updates follows from configuration rather than a switch:
a webhook when `TELEGRAM_WEBHOOK_SECRET` is set, long polling otherwise. Polling
needs nothing but an outbound connection, which is what makes the feature
developable on localhost.

---

# Storage

## PostgreSQL

Stores:

- users
- conversations
- goals
- reports
- check-ins
- integrations
- metadata

---

## Object Storage

Stores:

- PDFs
- Images
- Videos
- Audio

Production:

- S3
- Cloudflare R2

Development:

- MinIO

---

# Streaming

AI responses should be streamed.

Preferred technology:

```
Server Sent Events (SSE)
```

Reasons:

- Simple
- Native browser support
- Excellent for SSR
- Lower complexity than WebSockets

---

# Background Workers

Workers are responsible for asynchronous tasks.

Examples:

- Daily Briefings
- Weekly Reports
- Reminder Emails
- Strava Sync
- OCR
- Video Processing
- Embedding Generation

Workers should never contain business rules.

They invoke services.

---

# Project Structure

North uses **vertical slices**, not a flat `handlers/services/repository` split.

A slice owns one business capability end to end:

```
internal/<feature>/

    handler.go      parse, validate, authenticate, call service, render
    service.go      business logic
    repository.go   persistence
    db/queries.sql  sqlc input
    db/*.go         sqlc output
```

The layering rules in this document still hold — they are enforced *within* a slice.
A handler in `internal/goals` may call `goals.Service`. It may not call
`goals.Repository`, and no repository may call any service.

Cross-cutting code lives under `internal/shared/`:

```
internal/shared/

    database/    pgxpool construction
    errors/      sentinel errors mapped to HTTP status in one place
    middleware/  request id, logging, recover, CSRF, RequireAuth
    types/       types shared by more than one slice
```

Full tree:

```
cmd/
    web/
    worker/
    mcp-server/

internal/
    ai/          client interface, registry, gemini, openrouter, fake, prompts
    auth/
    users/
    coach/
    conversations/
    checkins/
    goals/
    workouts/
    reports/
    fitness/
    documents/
    media/
    search/
    integrations/
    messaging/
    config/
    shared/

web/
    shared/layout/
    shared/ui/
    auth/
    chat/
    workouts/
    landing/
    assets/

migrations/
terraform/
charts/
docs/
```

**Why slices rather than layers.** A layered tree makes every feature a diff across
four directories and makes it easy to accidentally reach sideways. A slice makes the
boundary the feature, which is the boundary that actually changes. Adding Telegram or
a new fitness provider should mean adding a directory, not editing five.


---

# Deployment

Infrastructure stack:

```
GitHub

↓

GitHub Actions

↓

GHCR

↓

ArgoCD

↓

Helm

↓

k3s

↓

North
```

Infrastructure is managed using Terraform.

---

# Design Decisions

## Why Go?

- Fast
- Simple
- Excellent concurrency
- Single binary deployment
- Great tooling

---

## Why Templ?

- Type-safe
- Readable
- Excellent SSR support
- Strong Go integration

---

## Why HTMX?

- Minimal JavaScript
- Progressive enhancement
- Excellent fit for SSR

---

## Why SSE?

- Simpler than WebSockets
- Native browser support
- Perfect for AI streaming

---

## Why PostgreSQL?

- Reliable
- Mature
- Excellent SQL support
- Easy to scale

---

## Why sqlc?

- Explicit SQL
- Compile-time safety
- No ORM magic

---

## Why Kubernetes?

North is expected to evolve into multiple deployable services:

- Web
- Workers
- MCP Server
- OCR
- Embeddings
- Media Processing

Kubernetes provides a stable deployment platform while allowing the application itself to remain modular rather than distributed.

---

# Future Evolution

North should evolve through composition.

Future capabilities may include:

- Native mobile apps
- Apple Watch
- Widgets
- Live Activities
- Voice conversations
- Vision-based coaching
- Team coaching
- Family coaching
- Enterprise deployments

The core architecture should remain unchanged.

Only interfaces and integrations should grow.

---

# Architectural Rule

**Every new feature should answer one question:**

> **Can this be implemented by extending existing services and interfaces, or does it unnecessarily complicate the architecture?**

When in doubt, prefer the simpler solution.

North should remain understandable, modular, and enjoyable to maintain for many years.