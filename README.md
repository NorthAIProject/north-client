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

```
cmd/
    web/
    worker/
    mcp-server/

internal/

    ai/
    auth/
    coach/
    conversations/
    goals/
    reports/
    fitness/
    documents/
    media/
    integrations/
    messaging/
    repository/
    handlers/
    jobs/
    domain/

web/

    templates/
    components/
    pages/
    layouts/
    static/

terraform/

charts/

docs/

migrations/
```

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

MIT# north-client
