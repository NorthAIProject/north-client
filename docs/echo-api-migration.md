# Echo API migration

The JSON API moved from chi to [Echo v5](https://echo.labstack.com/). The
browser app (Templ + HTMX, on chi) has since been replaced by the SvelteKit
front end in `north-web-app` and deleted, so this repository serves only the
API and chi is gone.

## Shape of the process

```text
echox.Serve(ctx, srv)                 graceful shutdown on SIGTERM (echo.StartConfig)
└── *echo.Echo (echox.New)
    ├── RequestID → RequestLogger → Recover → Secure   every request
    ├── IPExtractor (TrustedProxies)            what c.RealIP() answers
    ├── ErrorHandler                            every error a handler returns
    ├── /api/v1   Locale + Gzip (not streams)   feature APIs, natively on Echo
    │   ├── public        BodyLimit(1 MiB)
    │   ├── bearer        BodyLimit(1 MiB) + auth.Bearer
    │   └── uploads       BodyLimit(media) + auth.Bearer
    ├── /mcp, /oauth (+ CORS), /ingest/health, /webhooks/telegram
    ├── /healthz, /internal/metrics (METRICS_SECRET)
    └── anything else                           Echo's JSON 404
```

## Conventions for a feature API

A feature package's `api.go` registers on an `*echo.Group` the composition
root hands it and never chooses its own middleware for authentication or body
size — the group already carries them.

```go
func (a *API) Register(g *echo.Group)        // was Routes(r chi.Router)
func (a *API) RegisterPublic(g *echo.Group)  // was PublicRoutes
func (a *API) RegisterUploads(g *echo.Group) // was UploadRoutes
```

| chi                                         | Echo                                                   |
| ------------------------------------------- | ------------------------------------------------------ |
| `r.Get("/goals/{goalID}", a.show)`          | `g.GET("/goals/:goalID", a.show)`                      |
| `r.With(guard).Post(p, h)`                  | `g.POST(p, h, quotas.GuardAPI(action))`                |
| `func (a *API) h(w, r)`                     | `func (a *API) h(c *echo.Context) error`               |
| `r.Context()`                               | `c.Request().Context()`                                |
| `chi.URLParam(r, "id")`                     | `c.Param("id")`                                        |
| `r.URL.Query().Get("x")`                    | `c.QueryParam("x")`                                    |
| `httpx.WriteJSON(w, status, v)`             | `return c.JSON(status, v)`                             |
| `w.WriteHeader(http.StatusNoContent)`       | `return c.NoContent(http.StatusNoContent)`             |
| `httpx.Error(w, err, "msg"); return`        | `return httpx.Fail(err, "msg")`                        |
| `httpx.ReadJSON(w, r, &v, opts)` + Error    | `echox.BindJSON(c, &v, opts, "msg")`                   |
| uuid path param → 404 on parse failure      | `echox.UUIDParam(c, "goalID")`                         |
| `http.Redirect(w, r, url, code)`            | `return c.Redirect(code, url)`                         |
| a hand-written `httpx.ErrorBody` reply      | keep it: `return c.JSON(status, httpx.ErrorBody{…})`   |

Rules:

- **Bodies and statuses do not change.** The shipped iOS app decodes them.
  `httpx.Fail` + `echox.ErrorHandler` write exactly what `httpx.Error` wrote;
  every message string stays word for word.
- Helpers that wrote an error and returned `bool` now return `error`
  (`readGoal(c) (Input, error)`), and callers `return err` unchanged — the
  error is already a `httpx.Failure`.
- A handler never writes a reply *and* returns a non-nil error.
- Streaming (coach SSE) writes through `c.Response()` and flushes with
  `http.NewResponseController(c.Response()).Flush()`; once the stream has
  started, errors are written as events, not returned.
- Tests build the routes with `e := echo.New()` (or `echox.New`) and
  `api.Register(e.Group("/api/v1"))`, and serve with `e.ServeHTTP`.
- `goals` is the reference conversion: `internal/goals/api.go`.

## Echo's middleware, not ours

Cross-cutting HTTP concerns use Echo's built-ins, configured once in
`internal/shared/echox` or at the group that needs them. Do not hand-write a
replacement.

| Concern            | Echo built-in                         | Where                                    |
| ------------------ | ------------------------------------- | ---------------------------------------- |
| Request id         | `RequestIDWithConfig`                 | `echox.RequestID`                        |
| Access log         | `RequestLoggerWithConfig`             | `echox.RequestLogger`                    |
| Panics             | `Recover`                             | `echox.New`                              |
| Hardening headers  | `SecureWithConfig`                    | `echox.Secure` — same values as Traefik  |
| Compression        | `GzipWithConfig`                      | `echox.Gzip` on `/api/v1`, skips SSE     |
| Body size          | `BodyLimit`                           | each group in `cmd/api`                  |
| CORS               | `CORSWithConfig`                      | `mcpauth.machineCORS` (OAuth endpoints)  |
| Client address     | `ExtractIPFromXFFHeader` + `TrustIPRange` | `TrustedProxies.IPExtractor`, `echo.Config.IPExtractor` |
| Rate limit buckets | `RateLimiterMemoryStore`              | `ratelimit.New` (also used by services)  |
| Graceful shutdown  | `StartConfig.Start`                   | `echox.Serve` (`cmd/api`, `cmd/mcp-server`) |

Two settings differ from Echo's defaults on purpose:

- The IP extractor trusts **only** `TRUSTED_PROXIES`. Echo's default trusts
  every loopback, link-local and private peer, which on a cluster network is
  every pod.
- A route that streams Server-Sent Events goes in `apiStreams`
  (`cmd/api/api.go`) so gzip never wraps it; a test fails if an entry stops
  matching a route.

## Repositories and dependency injection

Every service depends on the persistence it needs through an interface
declared in its own package, and is handed a concrete Postgres repository by
the composition root. There is no container library: wiring is explicit Go in
one place.

```go
// internal/goals/service.go
type Repository interface {
	Create(ctx context.Context, userID uuid.UUID, g NewGoal) (Goal, error)
	// … only what Service calls
}

func NewService(repo Repository) *Service

// internal/goals/repository.go
type PostgresRepository struct{ q *goalsdb.Queries }

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository
```

The interface lists only the methods the service calls, in the order they
appear in `repository.go`. A repository method nothing in the service calls
stays on the struct and off the interface. `goals` is the reference.

`NewRepository` keeps its name, so call sites read the same; what changes is
that a service can be tested against a fake and no longer knows Postgres
exists.

## Order of work

1. Echo in front, chi behind it (done: `a719be2a`).
2. Every feature API onto Echo natively; `/api/v1` stops reaching chi.
3. `/mcp`, OAuth's machine half, health ingest and the Telegram webhook onto
   Echo groups with their own body limits.
4. Repository interfaces in every service; one composition root shared by
   `cmd/api` and `cmd/worker`.
5. Done: the browser app (`web/`, Templ, HTMX, CSRF, chi) is deleted and
   `cmd/web` is `cmd/api`. The image still names the binary `/app/main`.
