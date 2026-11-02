# Web rendering separation effort

## Decision summary

Separating North's web rendering from Go and moving the web application to TanStack Start + Solid, while keeping Go as an API/backend service behind Echo, is feasible and strategically coherent. It is a substantial frontend rewrite, not a small framework migration.

Recommended target:

```text
Browser
  |
  v
TanStack Start + Solid
  - route tree
  - SSR and hydration
  - browser interactions
  - page loaders / server functions
  - public web assets
  |
  | same-origin proxy or private service-to-service calls
  v
Go + Echo
  - JSON API
  - authentication/session authority
  - domain services
  - repositories and PostgreSQL
  - SSE / uploads / webhooks / MCP
  - workers and integrations
```

The Go service should remain the source of truth for identity, authorization, business rules, persistence, AI, integrations, and long-running streams. TanStack Start should own document rendering and browser state, not duplicate North's domain logic.

## Current state

North is currently one Go process serving both the backend and the web presentation layer:

- Routing is centralized in `cmd/web/routes.go` using Chi.
- The process mounts `/api/v1`, `/mcp`, health ingestion, webhooks, public pages, authentication pages, and the authenticated `/app` tree.
- The authenticated route group mounts roughly thirty feature handlers, each of which can expose both page routes and HTML-fragment mutations.
- Pages are authored in 165 `.templ` files with 164 generated Go files.
- The web tree contains 283 Go files and extensive render tests.
- There are approximately 677 route registration calls in the Go web/API code and 100 feature-level `Routes` methods.
- HTMX, Alpine, and small custom scripts form the browser interaction model. There are 113 files with HTMX/`HX-*` references.
- At least 31 files participate in streaming/SSE behavior. Coach streaming currently sends HTML fragments, not JSON, so HTMX can insert tokens directly into the DOM.
- Assets, Tailwind CSS generation, Templ generation, and the Go binaries are assembled into one Docker image.

Relevant code locations:

- `cmd/web/routes.go:1038-1320` — process-wide route and middleware composition.
- `cmd/web/http_handlers.go:107-170` — embedded/static asset serving and compressed exercise-frame rewriting.
- `internal/coach/handler.go:401-590` — HTML-over-SSE coach streaming.
- `internal/shared/middleware/csrf.go:35-166` — double-submit CSRF implementation and cookie issuance.
- `web/shared/layout/app.templ` and `web/shared/layout/auth.templ` — shared page shells.
- `scripts/build-css.sh` and `Dockerfile` — current CSS, Templ, and binary build pipeline.
- `docs/api/openapi.yaml` — existing API contract that can become the frontend boundary.

The existing architecture documentation explicitly describes Go + Templ + Tailwind + HTMX + Alpine as the presentation stack. This proposal changes that presentation layer while preserving the lower layers.

## Target stack assessment

TanStack Start is a good fit for the requested frontend direction. The current Solid documentation describes full-document SSR, streaming, server routes, server functions, middleware, and client/server builds. TanStack Router remains the routing core. The framework also supports selective SSR, which is useful for browser-heavy areas such as media, charting, and rich coaching interactions.

Echo is a reasonable fit for the Go edge/API service. It provides routing, middleware, binding, and standard HTTP integration points. It does not remove the need to design North's authentication, authorization, CSRF, upload, SSE, and observability boundaries; it only changes the HTTP framework used to expose them.

Important constraint: TanStack Start SSR runs in the JavaScript runtime/build system. It cannot directly call Go services or Go domain packages during server rendering. The Start server must call the Go API over HTTP, or the deployment must place a proxy in front of both services.

Sources consulted:

- [TanStack Start](https://tanstack.com/start/latest)
- [TanStack Start Solid overview](https://tanstack.com/start/latest/docs/framework/solid/overview)
- [TanStack Start Solid server routes](https://tanstack.com/start/latest/docs/framework/solid/guide/server-routes)
- [TanStack Start Solid server functions](https://tanstack.com/start/latest/docs/framework/solid/guide/server-functions)
- [TanStack Start Solid middleware](https://tanstack.com/start/latest/docs/framework/solid/guide/middleware)
- [TanStack Start Solid authentication server primitives](https://tanstack.com/start/latest/docs/framework/solid/guide/authentication-server-primitives)
- [Echo documentation](https://echo.labstack.com/)

## What can be reused

Most of the application layer should not move:

- `internal/*/Service` implementations and repository code.
- PostgreSQL schema and migrations.
- AI provider abstraction, prompt system, quotas, jobs, storage, integrations, and workers.
- Existing native-app JSON API behavior.
- OpenAPI contract and API-level tests, after any contract gaps are closed.
- Domain types where they can be represented as JSON DTOs without exposing persistence internals.
- Go-side security controls, provided they are exposed through explicit API/session contracts.

The main reusable asset is the existing API surface. The main non-reusable asset is the Templ/HTMX presentation code.

## What must be rewritten or redesigned

### 1. Page rendering

Every user-facing Templ page must become a Solid route/component. Templ markup cannot be mechanically converted into useful Solid code because the current templates contain Go conditionals, server-side data shaping, HTML fragment boundaries, form conventions, localization calls, and component composition that are specific to Go.

The shared app shell is a good first migration target. After that, pages should be migrated by vertical slice: route loader, API DTOs, Solid components, mutations, loading/error states, analytics, and tests together.

### 2. HTMX fragment protocol

The current server frequently answers mutations with HTML fragments, redirects, `HX-*` headers, or out-of-band swaps. TanStack Start should replace this with typed JSON responses and client-side state updates. Some actions can use native forms progressively enhanced by Start server functions, but most authenticated app interactions will be clearer as explicit API calls backed by TanStack Query or route loaders.

This is a behavioral rewrite, not just a template rewrite. Each HTMX target/swap needs an intentional Solid state transition.

### 3. Coach and other streams

Coach streaming currently emits HTML over SSE so HTMX can place tokens directly in the transcript. The backend should emit a stable event protocol, for example:

```text
message.delta       { text }
tool.status         { label, state }
message.failed      { code, message }
message.completed   { messageID }
```

Solid owns transcript rendering and stream lifecycle. The Go service should continue owning model execution, quota consumption, persistence, cancellation, and authorization. This is a high-risk area because it changes buffering, reconnect, error, and cancellation behavior.

### 4. Authentication and browser session boundary

The current browser flow relies on Go middleware, session cookies, account lookup, CSRF cookies, locale/appearance cookies, auth redirects, OAuth callbacks, WebAuthn, password reset, and invite redemption.

The recommended design is to keep session authority in Go and make the Start server a same-origin web frontend. Options, in descending order of preference:

1. Put a reverse proxy or ingress in front of both services, keep the browser on one origin, and let Go issue/validate the existing secure session cookie.
2. Let Start proxy authenticated browser requests to Go without exposing a second public origin.
3. Only if required, introduce a new frontend session cookie and a short-lived backend token exchange. This adds complexity and should not be the default.

Do not move auth logic into browser code or rely only on TanStack route guards. TanStack's own guidance treats route guards as a UX layer and requires authorization at every server function/server route/API data boundary.

The CSRF contract must be explicit. Same-origin cookies plus a Go API need either the existing double-submit token carried by the Solid client or a carefully scoped same-origin protection strategy for every state-changing request.

### 5. API and DTO boundary

The current APIs are already the best seam, but the web currently bypasses them by calling Go services directly inside page handlers. The frontend migration requires every page loader and mutation to use a documented HTTP contract.

Work includes:

- inventorying endpoints that exist only as HTML handlers;
- adding JSON equivalents where the API is incomplete;
- separating presentation view models from database/domain structs;
- defining consistent error, pagination, validation, and authorization responses;
- generating or maintaining TypeScript types from OpenAPI;
- deciding which calls are direct browser-to-Go API calls and which are Start server-side calls.

### 6. Assets, CSS, PWA, and deployment

The current Go binary embeds assets and performs special handling for compressed exercise SVGs. The new frontend build must own static assets, Tailwind scanning, fonts, PWA files, cache headers, exercise-frame serving, and source maps. Large exercise assets should likely be emitted as immutable static files or moved to object storage/CDN rather than duplicated into both service images.

The current one-image deployment must become at least two independently built runtime artifacts: a Go API/worker image and a Start web image. Health checks, logs, metrics, tracing, environment variables, local development, migrations, and rollout order all need updates.

## Effort estimate

Estimate assumes one experienced Go engineer and one experienced Solid/TypeScript engineer, existing product behavior is preserved, no major redesign of the information architecture, and the backend remains in the current repository.

| Workstream | Effort | Notes |
| --- | ---: | --- |
| Boundary/design spike | 1–2 weeks | API ownership, cookie model, proxy topology, streaming protocol, DTO rules |
| Echo backend foundation | 1–2 weeks | New entrypoint, middleware, health, logging, metrics, graceful shutdown, route groups |
| API extraction/adaptation | 2–4 weeks | Move/reshape browser-needed endpoints; preserve native API behavior; add contract tests |
| Auth, CSRF, OAuth, WebAuthn, invites | 2–4 weeks | Highest security-sensitive area; can remain Go-owned but needs frontend integration |
| Start/Solid foundation | 1–2 weeks | Vite, route tree, SSR, layouts, API client, query/cache, errors, i18n, analytics |
| Public/auth pages | 1–2 weeks | Landing, legal, login, signup, reset, consent, redirects |
| Authenticated shell and navigation | 1–2 weeks | App layout, locale, appearance, voice capability, onboarding gate, responsive states |
| Main product pages | 8–14 weeks | Dashboard, day, coach, goals, check-ins, memories, knowledge, settings, fitness, nutrition, etc. |
| Interaction and stream migration | 3–5 weeks | HTMX mutations, uploads, SSE, live crews, optimistic/pending/error states |
| Assets/PWA/build/deployment | 2–3 weeks | Static assets, exercise frames, Docker, ingress, staging, cache policy |
| Parity, accessibility, performance, rollout | 3–5 weeks | Visual/behavioral regression, mobile browsers, SSR/hydration, observability, canary |
| **Total full-parity effort** | **24–40 engineer-weeks** | Parallelized calendar time is approximately 12–20 weeks with two strong engineers |

The range is wide because the unknown is not TanStack or Echo. The unknown is the number of HTML-only behaviors hidden in feature handlers and templates, especially forms that return different fragments depending on `HX-Request`, stream edge cases, and auth/OAuth flows.

### Smaller incremental option

A low-risk first release can be delivered in 8–12 engineer-weeks by:

- introducing Echo only for a new `/api/v2` or frontend-specific API surface;
- keeping the current Go/Templ web live;
- building a Start/Solid shell for one vertical slice, preferably dashboard or coach;
- using the existing Go session cookie through a same-origin proxy;
- migrating no more than 10–15 pages initially.

This proves the runtime, auth, API, SSR, asset, and streaming model before committing to all-page parity.

## Suggested migration sequence

### Phase 0 — architecture spike

Build a throwaway or small production-shaped Start app that can:

- SSR a protected route;
- read the current Go session cookie;
- call one authenticated Go endpoint;
- submit a CSRF-protected mutation;
- render a streamed coach response;
- run behind the intended ingress topology.

The phase is successful only when the browser sees one origin and the Go API never trusts client-supplied user identity.

### Phase 1 — establish the backend contract

Create the Echo service boundary without deleting the existing Chi web server. First move infrastructure endpoints and API routes that are already JSON. Keep the domain/service wiring in Go and add API contract tests. Echo can coexist during the migration; there is no value in changing all route registration before the frontend needs the endpoint.

### Phase 2 — migrate the shared frontend foundation

Create the Start app, shared layouts, design tokens, API client, generated types, i18n, auth bootstrap, error boundaries, loading states, analytics, PWA behavior, and static asset pipeline. Migrate public/auth pages first because they establish cookies and redirect behavior without the full authenticated app complexity.

### Phase 3 — migrate vertical slices

Recommended order:

1. Dashboard / My Day — validates SSR loaders, navigation, cards, and common mutations.
2. Check-ins / goals — validates forms, validation, optimistic states, and redirects.
3. Coach — validates SSE, uploads, quota errors, tool confirmations, and cancellation.
4. Knowledge / memories / settings — validates rich forms, file uploads, pagination, and account preferences.
5. Workouts / nutrition / fitness / insights — validates large data views, charts, media, and complex editors.
6. Crews / moderation / live views — validates long-lived streams and multi-user refreshes.

Keep each migrated slice behind a route-level or ingress-level flag so old and new behavior can be compared.

### Phase 4 — retire Go rendering

After parity tests and production traffic prove the new frontend, remove HTML-only handlers and Templ generation from the Go build. Keep Go handlers only where they are still needed for external clients, redirects/webhooks, streams, or compatibility during a deprecation window.

## Main risks and mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Auth/session split | Security and rollout risk | Keep Go as session authority; same-origin proxy; security tests before page migration |
| HTML-only endpoint behavior | Hidden parity gaps | Inventory `HX-*` branches and migrate endpoint-by-endpoint with contract tests |
| HTML-over-SSE coupling | Coach regressions | Define versioned JSON event protocol; test reconnect, cancellation, quota, and provider errors |
| Chi-to-Echo rewrite scope | Backend churn with little product value | Extract API behavior first; use adapters where safe; migrate route registration incrementally |
| SSR data duplication | Stale or inconsistent pages | One typed API client and one cache/query policy; avoid separate Start-only domain logic |
| Two-service operations | More deployment failure modes | Shared request IDs, tracing, health checks, dashboards, local compose environment |
| Asset duplication | Larger builds and cache misses | Serve immutable assets from Start/CDN; retain compressed exercise frames without copying them unnecessarily |
| SSR/hydration mismatch | Visual or runtime errors | Prefer server-safe loaders; selectively disable SSR only for browser-only features; add hydration smoke tests |

## Recommendation

Proceed, but treat this as a staged product-platform migration. Do not begin by rewriting all Go route registration from Chi to Echo. The high-value sequence is:

1. define the same-origin and session model;
2. prove one Start/Solid SSR page against the current Go API;
3. extract the missing JSON contracts;
4. migrate by product vertical;
5. move Echo underneath the API boundary as needed;
6. retire Templ/HTMX only after traffic and parity evidence.

The project should be approved as a medium-to-large migration: approximately 24–40 engineer-weeks for full web parity, or 8–12 engineer-weeks for a first vertical-slice release that validates the architecture. The effort is justified if North's web roadmap needs richer client interactions, a TypeScript/Solid frontend team, or a clearer separation between product UI and Go application services. It is not justified solely to replace Chi or to obtain SSR; North already has both routing and SSR today.

## Open decisions before implementation

1. Will the public deployment use one origin with an ingress proxy, or will the browser call a separate API origin?
2. Does Go remain the sole issuer and validator of browser sessions?
3. Will browser requests call Echo directly, or will Start server routes proxy to Echo?
4. Is coach streaming standardized as JSON SSE, or should it move to another transport?
5. Which pages are in the first vertical slice?
6. Will the existing public URLs and HTML semantics be preserved exactly, or is this also a UX redesign?
7. Should exercise art and other large assets move to object storage/CDN during the split?
8. Is Echo a strategic backend decision, or only the HTTP framework for the new API entrypoint?

