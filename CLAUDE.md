# CLAUDE.md

> This document defines the engineering principles, architectural decisions, and coding standards for AI agents contributing to North.
>
> Every implementation should prioritize **clarity, simplicity, maintainability, and long-term evolution** over clever abstractions or unnecessary complexity.

---

# Project Overview

North is an AI Operating System for Personal Growth.

It combines:

- Long-term conversational memory
- Goal management
- AI coaching
- Knowledge management
- Fitness integrations
- MCP integrations
- Multi-platform messaging

The application is **SSR-first**, written in Go, and designed around a clean layered architecture.

---

# Core Philosophy

North is **not** an AI demo.

North is **not** a CRUD application with an LLM attached.

North is an intelligent system that helps users improve their lives over months and years.

Every architectural decision should reinforce this vision.

---

# Engineering Principles

## Simplicity wins

Prefer simple code over clever code.

Good code should be understandable six months from now without needing additional explanation.

Avoid unnecessary abstractions.

---

## Explicit is better than implicit

Prefer:

- explicit types
- explicit dependencies
- explicit interfaces
- explicit error handling

Avoid magic.

---

## Composition over inheritance

Use composition.

Avoid deep abstraction hierarchies.

---

## Build features before frameworks

North should never depend on large frameworks that reduce flexibility.

Prefer Go's standard library whenever practical.

---

# Architecture

North follows a layered architecture.

```
HTTP

↓

Handlers

↓

Services

↓

Repositories

↓

Database
```

External systems:

```
LLM Providers

MCP

Object Storage

Messaging Platforms

Fitness APIs
```

are infrastructure.

They should never contain business logic.

---

# Dependency Rules

Allowed direction:

```
Handlers

↓

Services

↓

Repositories

↓

Database
```

Never reverse dependencies.

Repositories must never call services.

Services must never import handlers.

---

# Handlers

Handlers should remain extremely small.

Responsibilities:

- parse request
- validate request
- authenticate user
- call service
- render template

Nothing more.

Business logic does **not** belong inside handlers.

---

# Services

Services contain application logic.

Examples:

- CoachService
- GoalService
- CheckInService
- ReportService
- FitnessService
- DocumentService

Services coordinate repositories and infrastructure.

---

# Domain

The domain layer contains business concepts.

Examples:

- User
- Goal
- Conversation
- Message
- Report
- CheckIn

The domain layer should remain independent of:

- SQL
- HTTP
- HTML
- AI Providers

---

# Repositories

Repositories own persistence.

Responsibilities:

- querying
- inserting
- updating
- deleting

Repositories should not contain business rules.

---

# AI

The AI layer is replaceable.

Never couple business logic directly to:

- OpenAI
- Anthropic
- Gemini
- xAI

Instead depend on interfaces.

Example:

```go
type AIClient interface {
    Chat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
}
```

Switching providers should never require changes to services.

---

# Prompt Engineering

Prompts are first-class citizens.

Store prompts inside:

```
internal/ai/prompts/
```

Never embed prompts inside:

- handlers
- repositories
- templates

Prompts should be:

- versioned
- readable
- modular
- reusable

---

# Context Building

North's intelligence comes from context.

The ContextBuilder is responsible for collecting:

- User profile
- Goals
- Recent conversations
- Check-ins
- Reports
- Calendar
- Fitness
- Documents
- MCP tools

before every AI request.

Never build prompts directly from handlers.

---

# Memory

Memory is a product feature.

Treat memory carefully.

Do not discard historical information unless explicitly requested.

Conversation history should be summarized instead of deleted whenever possible.

---

# Knowledge

Documents are knowledge sources.

Supported formats:

- PDF
- Markdown
- Images
- Video
- Audio
- Text

Knowledge should be searchable using embeddings.

Never send entire documents to the LLM unless absolutely necessary.

Retrieve relevant context first.

---

# MCP

North acts as both:

- MCP Client
- MCP Server

The MCP server should expose business capabilities—not database tables.

Good examples:

- Search Goals
- Create Check-in
- Weekly Review
- Search Knowledge
- Get Fitness Summary

Bad examples:

- Raw SQL
- Internal IDs
- Database-specific operations

---

# Messaging

Messaging platforms are interfaces.

Examples:

- Telegram
- Discord
- WhatsApp

Every message should eventually reach the same CoachService.

Avoid platform-specific business logic.

---

# Fitness

Fitness providers are interchangeable.

Examples:

- Strava
- Apple Health
- Garmin
- Fitbit

The CoachService consumes summaries—not raw API responses.

---

# Templates

North uses Templ.

Guidelines:

- small components
- reusable layouts
- readable HTML
- semantic structure

Avoid giant template files.

---

# HTMX

Prefer HTMX over JavaScript.

Most interactions should be implemented using:

- forms
- partial rendering
- SSE
- HTMX swaps

---

# Alpine.js

Use Alpine only for local UI state.

Examples:

- modal visibility
- dropdowns
- tabs
- form helpers

Business logic belongs on the server.

---

# JavaScript

Less JavaScript is better.

Avoid introducing frontend frameworks.

If HTMX solves the problem, use HTMX.

---

# CSS

Use Tailwind.

Avoid custom CSS unless there is a compelling reason.

---

# SQL

Use sqlc.

Prefer explicit SQL.

Avoid ORMs.

---

# Error Handling

Always return meaningful errors.

Never silently ignore failures.

Wrap errors with context where appropriate.

---

# Logging

Use structured logging.

Never commit debugging statements.

Never log:

- passwords
- API keys
- OAuth tokens
- secrets

---

# Testing

Focus tests on:

- Services
- ContextBuilder
- PromptBuilder
- Repositories
- Business logic

HTTP handlers require minimal testing.

---

# Performance

Do not optimize prematurely.

Measure first.

Use streaming responses (SSE) for AI output.

---

# Dependencies

Every dependency introduces maintenance cost.

Before adding a dependency ask:

1. Can the standard library solve this?
2. Does this simplify the architecture?
3. Is it actively maintained?
4. Does it improve readability?

If the answer is no, don't add it.

---

# Infrastructure

Deployment targets Kubernetes.

Stack:

- k3s
- Helm
- ArgoCD
- Terraform

Application code should never depend on Kubernetes.

The application should run equally well locally using Docker Compose.

---

# Security

Always consider:

- Authentication
- Authorization
- CSRF
- XSS
- SQL Injection
- Rate limiting

Never expose secrets.

---

# Code Style

Prefer:

- small functions
- meaningful names
- composition
- explicit interfaces
- readable code

Avoid:

- global state
- reflection
- unnecessary generics
- deeply nested logic
- premature abstraction

---

# Decision Making

When multiple implementations are possible, choose the one that is:

1. Easiest to understand.
2. Easiest to maintain.
3. Most idiomatic Go.
4. Most aligned with existing architecture.

---

# Long-Term Goal

North should remain understandable by a new Go developer within a single afternoon.

The architecture should grow naturally through composition—not constant rewrites.

Every contribution should leave the codebase simpler than it was before.