# Go Backend Engineering Audit — north-client (Khepri)

| | |
|---|---|
| **Date** | 2026-11-03 |
| **Source revision** | `bbb4b7d713188228d9077d41d908b90021dafc8c` (`main`, clean worktree) |
| **Production revision** | `5464d0c569aacdfb25ae8add3440db97394acb83` per `platform/infra/apps/khepri/worker/values-production.yaml`. Findings are stated against HEAD, so check each one against the deployed SHA before treating it as live. |
| **Toolchain** | `go1.27.3 darwin/arm64`, module `github.com/NorthAIProject/north-client` |
| **Scope** | `cmd/api`, `cmd/worker`, `cmd/mcp-server`, and `internal/**`: about 1,700 Go files and 181k LOC (non-test). |
| **Mode** | Read-only audit. No source files were changed, and no database, migration, or external API was touched. |
| **Security** | Not in scope. No security audit was performed. Items that look security-relevant are flagged for a separate review and are not rated as security findings here. |

---

## 0. Implementation status (handoff, 2026-11-03)

The P1 fixes are under way on branch **`fix/audit-p1`**, cut from `main` at `bbb4b7d7`.

- Nothing is committed or pushed. Every change is uncommitted in the main checkout.
- No git worktree was created.
- A disposable Postgres container, `north-audit-testdb` (pgvector/pgvector:pg17, `127.0.0.1:5491`), was started for the DB-backed tests and is still running. Reuse it or remove it with `docker rm -f north-audit-testdb`.

Run tests with:

```bash
export TEST_DATABASE_URL='postgres://north:north@127.0.0.1:5491/north?sslmode=disable'
```

| Finding | State | Verified |
|---|---|---|
| F-02 job timeout loop | **Done** | Yes. `go test -race ./internal/jobs/` passes. Both new tests failed on the old code and pass on the fix. |
| F-03 chunk re-index 23505 | **Done** | Yes. The new test reproduced `23505 document_chunks_document_id_ordinal_key` on the old code. `go test -race ./internal/documents/... ./internal/vault/...` passes. |
| F-04 Anthropic failover | **Done** | Yes. The new tests failed on the old code. `go test -race ./internal/ai/...` passes, including the existing classification tests. |
| F-01 Telegram dropped updates | **Partial. The tests have not been run.** | No. See the remaining work below. |
| Everything else (F-05 … F-32) | Not started | — |

### Done: F-02 (`internal/jobs`)

- **`worker.go`**
  - `Complete`, `Fail` and `Retry` now run on their own `bookkeepingTimeout` (10 s) context, detached from the possibly expired `runCtx`.
  - The job timeout is now a field (`jobTimeout`), with a tests-only `WithJobTimeout`.
- **`db/queries.sql` `ReleaseStaleJobs`**
  - A stale job with `attempts >= max_attempts` is now set to `failed`, with `last_error = 'abandoned while running on its last attempt; last error: …'`, instead of being requeued.
  - sqlc was regenerated (v1.31.1).
- **Tests added to `jobs_test.go`:**
  - `TestAStaleJobOnItsLastAttemptIsFailedNotRequeued`
  - `TestAJobThatTimesOutIsStillScheduledForRetry`

### Done: F-03 (`internal/documents`)

- **`repository.go`**
  - `PostgresRepository` now holds the pool.
  - `ReplaceChunks` builds the params first, then runs in one transaction: `DeleteChunksNotIn`, then `UpsertChunk` for each chunk, then commit. Readers keep seeing the old chunks until the commit.
  - The doc comment was rewritten to explain why the order matters.
- **`db/queries.sql`:** the `DeleteChunksNotIn` comment was updated, and sqlc regenerated.
- **Test added to `index_test.go`:** `TestReplacingChunksAfterAnEditKeepsUntouchedIDs`.

### Done: F-04 (`internal/ai/anthropic/errors.go`)

- A non-API error (transport, DNS, reset or EOF) is now wrapped as `ErrUnavailable`. The exceptions are `context.Canceled` and `context.DeadlineExceeded`, which are left unmarked so the runner stops.
- New `transientType` and `apiErrorType` helpers map body error types `overloaded_error`, `api_error` and `rate_limit_error` to `ErrUnavailable` at any status. This covers in-stream errors that arrive with status 200.
- **Tests added to `errors_test.go`:**
  - `TestAnOverloadInsideTheStreamFailsOver`, for both the managed client and a personal key
  - `TestANetworkFailureFailsOver`
  - `TestACancelledRequestDoesNotFailOver`

### Partial: F-01 (Telegram seen-set). Pick up here.

**Written, not yet verified:**

1. **New migration `migrations/20261103120000_messaging_seen_updates.sql`**
   - Creates table `messaging_seen_updates (platform, account_id, update_id, seen_at)`, with the first three as the primary key, plus an index on `seen_at`.
   - Backfills each link's current `last_update_id` into the table, so the first redelivery after the deploy is not answered twice.
2. **`internal/messaging/db/queries.sql`**
   - `ClaimMessagingUpdate` is rewritten as one statement: a CTE inserts into the seen-set with `ON CONFLICT DO NOTHING`, and that insert is the claim. It then updates `messaging_links` and returns `ml.*`.
   - `last_update_id` is now informational only. It is set to `greatest(...)`, and reset when the account changes.
   - New `PurgeSeenMessagingUpdates :execrows`.
   - sqlc was regenerated. The generated param struct now has `UpdateID` where it had `LastUpdateID`.
3. **`internal/messaging/repository.go`**
   - `ClaimUpdate` passes `UpdateID`.
   - New `PurgeSeenUpdates(ctx, olderThan)`.
   - `go build ./...` passed after this step.
4. **`internal/messaging/account_scope_test.go`** was rewritten to the new rule: an id never seen is accepted, a repeat is refused.
   - Added: `TestAnUpdateClaimedOutOfOrderIsStillAnswered`, `TestConcurrentDeliveriesOfOneUpdateClaimOnce`, `TestOldClaimsArePurged`, and a `linkedChat` helper.
   - Adjusted: `TestARedeliveryIsStillRefusedWithinOneBot` and `TestLegacyRowsAndAccountlessPlatformsStillWork`.
   - **The test run was interrupted, so none of this has been run yet.**

**Remaining work for F-01:**

1. Run `go test -race -count=1 ./internal/messaging/...` against the test DB and fix whatever fails. Pay particular attention to the CTE: the data-modifying `INSERT` inside `WITH` must be visible to the outer `UPDATE … FROM link, seen`. Also confirm that `service_test.go:193`, the redelivery test, still passes.
2. Prove the old code fails: stash the query, the generated code and the migration, and confirm that `TestAnUpdateClaimedOutOfOrderIsStillAnswered` fails.
3. Wire the purge into the worker:
   - Add a `jobs.KindSweepMessagingUpdates` kind.
   - Register a handler in `cmd/worker/main.go` that calls `messaging.NewRepository(pool).PurgeSeenUpdates(ctx, 48*time.Hour)`.
   - Add `RegisterPeriodic(24*time.Hour, …)`, placed next to `KindSweepQuotas`.
4. Serialise turns per chat in `internal/messaging/telegram/bridge.go`:
   - In `dispatch`, replace the bare `go func(){ b.answer(...) }()` with an ordered per-`ExternalID` queue, for example a `map[string][]func()` under a mutex with one drain goroutine per busy chat.
   - Create the `replyTimeout` context when a turn starts, not when it is queued.
   - `recover` and log each turn, which also covers part of F-08.
   - Add a bridge test: dispatch two updates for one chat, and assert that `Handle` never overlaps and runs in arrival order.
5. Update the comments that still describe the watermark:
   - `internal/messaging/doc.go:71`
   - `internal/messaging/telegram/poller.go:61`
   - the column comment on `last_update_id` (optional follow-up migration)
6. Run `go vet ./...`, `golangci-lint run ./...` and `go test -race ./...` across the module with `TEST_DATABASE_URL` set. That has not been done yet for the module as a whole.

### After F-01

1. Commit, one commit per finding. Open a PR against `main`.
2. Before that PR deploys, check each P1 against the production SHA, `5464d0c5`.
3. Next in roadmap order (§4): items 5–8, which are F-07/F-08 lifecycle helper, infra probes and grace period, F-09 rasterizer race, and F-05 jobs index.

---

## 1. Executive summary

**Overall:** the codebase is well above average for a Go service.

- Layering is clean and errors are wrapped consistently.
- Outbound clients all have timeouts and bounded body reads.
- The Postgres `SKIP LOCKED` queue is sound.
- pprof and metrics listen on separate loopback listeners.
- Every `pool.Begin` defers its rollback, and no request path uses `context.Background()`.
- `go vet` is clean, and `go test -race` passes on every package that runs without Postgres.

The problems cluster in a few areas:

- what happens **at the edges of time**: timeouts, shutdown, and retries
- **concurrency in the Telegram bridge**
- **unbounded growth** in the jobs table

No P0 issue was found: nothing is known to corrupt data at rest.

### Top risks

| Area | Top risk |
|---|---|
| Correctness | Telegram updates handled concurrently are **silently dropped** by a high-watermark claim (F-01). Editing a vault/indexed document makes **re-indexing fail** on a unique-constraint violation (F-03). |
| Reliability | A job that hits its 15-minute timeout can **never be marked failed**, so it re-runs forever (F-02). Rollouts kill in-flight coach replies, Telegram turns, and jobs without draining them (F-07). |
| Availability | Panics in errgroup or fire-and-forget goroutines **bypass Echo's Recover** and kill the single web pod (F-08). One case is a **reproduced data race** in the PNG rasterizer (F-09). |
| AI provider chain | With Anthropic first in production, **network errors and in-stream `overloaded_error` do not fail over** to the rest of the chain (F-04). SDK retries stack in front of the chain (F-12). |
| Database | `jobs` grows forever, and `HasPendingJobForUser` has no usable index. It runs on a request path and once per user in four sweeps (F-05). Health ingest makes up to 50k sequential round trips inside one transaction (F-06). |

### Top five interventions, by strength of evidence × impact

1. **F-01:** replace the Telegram `last_update_id` watermark with a seen-set, and serialise turns per chat.
2. **F-02:** do job bookkeeping on a fresh context, not on the expired `runCtx`, and make `ReleaseStale` respect `max_attempts`.
3. **F-03:** in `ReplaceChunks`, delete stale chunks before inserting, in one transaction.
4. **F-04:** classify transport errors and in-stream API errors as `ErrUnavailable` in the Anthropic adapter.
5. **F-07 / F-08:** add one small lifecycle helper (recover, track, drain) for detached goroutines, and raise the pod grace period so the drain has time to finish.

### Not inspected or not verified

- **Database tests were not run.** No `TEST_DATABASE_URL` was set, so every testdb-backed test skipped. Findings that depend on Postgres behaviour (F-03, F-05, F-06, F-13 to F-17) were derived from SQL and schema, not executed.
- **No production metrics, `pg_stat_statements`, `EXPLAIN` plans, or load profiles were available.** Every performance claim below is either source-derived or labelled MEASURE FIRST.
- **Not reviewed in depth:** the iOS and web clients, the MCP tool surface semantics, `internal/export`, `internal/vault` beyond indexing, the templ and visualisation code, and `scripts/`.
- **Security review not performed** (see §6 for hand-offs).

---

## 2. Findings table

Status legend:

- **CONFIRMED DEFECT:** the defect follows directly from the source, or was reproduced.
- **CODE-SUPPORTED RISK:** the failure mode is visible in the code, but frequency or impact is unmeasured.
- **MEASURE FIRST:** a hypothesis.

| ID | Pri | Category | Status | Location | Summary |
|---|---|---|---|---|---|
| F-01 | P1 | Concurrency / messaging | CONFIRMED DEFECT | `internal/messaging/telegram/bridge.go:88-92`, `internal/messaging/db/queries.sql:22-29`, `internal/messaging/service.go:234-244` | Out-of-order Telegram updates are dropped as "redelivered" |
| F-02 | P1 | Reliability / jobs | CONFIRMED DEFECT (source) | `internal/jobs/worker.go:167-198`, `internal/jobs/db/queries.sql:76-79` | Timed-out jobs cannot be completed or failed, so they loop forever |
| F-03 | P1 | Database / correctness | CONFIRMED DEFECT (source) | `internal/documents/repository.go:116-151`, `internal/documents/db/queries.sql:90-100`, `migrations/20260808134439_…sql:103` | Re-indexing an edited document violates `UNIQUE(document_id, ordinal)` |
| F-04 | P1 | Reliability / AI | CONFIRMED DEFECT | `internal/ai/anthropic/errors.go:28-58` | Anthropic transport and in-stream errors never fail over |
| F-05 | P2 | Database / jobs | CODE-SUPPORTED RISK | `internal/jobs/db/queries.sql:59-79`, `migrations/00005_create_media_and_jobs.sql:70` | `jobs` has no retention; the pending-job check is unindexed; failed embed jobs are re-enqueued forever |
| F-06 | P2 | Database | CODE-SUPPORTED RISK | `internal/health/repository.go:41-67`, `internal/health/health.go:99` | Health ingest runs up to 50k sequential upserts in one transaction |
| F-07 | P2 | Lifecycle / shutdown | CODE-SUPPORTED RISK | `cmd/api/server.go:55,131,142-146`, `internal/coach/service.go:365,389`, `internal/messaging/telegram/bridge.go:88`, `internal/jobs/worker.go:167` | Rollouts kill detached work, and the job comes back only after 30 minutes |
| F-08 | P2 | Concurrency / availability | CODE-SUPPORTED RISK | ~150 `g.Go` sites (`internal/day/service.go:314-464`, `internal/dashboard/service.go:249-524`, …), `bridge.go:81,89`, `internal/settings/api.go:398` | A panic outside the handler goroutine kills the process |
| F-09 | P2 | Concurrency | CONFIRMED DEFECT (race reproduced) | `internal/shared/rasterize/text.go:35-36,82,105,124` | A cached `font.Face` is shared across goroutines |
| F-10 | P2 | Correctness / AI | CONFIRMED DEFECT | `internal/coach/confirm.go:250-298`, `internal/coach/api.go:447-461` | `/resume` has no state guard: duplicate replies, a free model turn, and the Haiku prefill shape |
| F-11 | P2 | Correctness / DB | CONFIRMED DEFECT | `internal/conversations/db/queries.sql:43`, `internal/conversations/service.go:22,133`, `internal/coach/api.go:265,284,589` | Threads over 200 messages show the oldest 200, and `done.messageId` points at the wrong message |
| F-12 | P2 | Reliability / AI | CODE-SUPPORTED RISK | `internal/ai/anthropic/client.go:73-79`, `internal/ai/providers/…:99-105`, `internal/coach/service.go:365` | SDK retries (×3, uncapped Retry-After) stack before failover, and there is no first-token budget |
| F-13 | P2 | Observability / AI cost | CONFIRMED DEFECT | `internal/coach/service.go:515,878`, `internal/conversations/summarize.go`, `internal/ai/meter.go:125-147` | Tool-round and summary spend is unattributed; the spend insert is synchronous with no deadline |
| F-14 | P2 | Database / pool | MEASURE FIRST | `internal/shared/database/database.go:24`, `internal/day/service.go:314-464`, `internal/dashboard/service.go:249-524`, `internal/coach/context_builder.go:298-327` | Per-request fan-out of 18–40 goroutines against `MaxConns=10`, with no pool metrics |
| F-15 | P2 | Ops / probes | CODE-SUPPORTED RISK | infra `apps/khepri/web/values-common.yaml:16-20`, `apps/khepri/worker/values-common.yaml:15-25` | Web liveness pings Postgres; worker liveness ignores the job loop |
| F-16 | P2 | HTTP / memory | CODE-SUPPORTED RISK | `internal/media/api.go:113-114`, `internal/media/storage.go:95` | A 90 MB video upload uses 70–100 MiB of heap per request in a 768 Mi pod |
| F-17 | P2 | HTTP / rate limit | CODE-SUPPORTED RISK | `cmd/api/api.go:105,107`, `internal/auth/session.go:99` | `/api/v1` bearer routes have no request limiter; every bad token costs a DB query |
| F-18 | P2 | Database | CONFIRMED DEFECT (index unusable) | `internal/conversations/db/queries.sql:72-88,131-148`, `migrations/20260814190000…:17` | The extraction sweep's partial index can never match its query; the summary sweep scans everything |
| F-19 | P2 | Correctness / AI | CODE-SUPPORTED RISK | `internal/coach/context_builder.go` (`Recent(…,20)`), `internal/conversations/conversations.go:259-267` | A 20-message window can start with an orphaned `tool_result`, which gets a 400 from the provider |
| F-20 | P2 | Database / vector | MEASURE FIRST | `internal/documents/db/queries.sql:258-279`, `migrations/20260808192429…:54` | Global HNSW plus a `user_id` post-filter can return few or zero hits for small users |
| F-21 | P3 | Jobs / throughput | MEASURE FIRST | `internal/jobs/worker.go:82`, `cmd/worker/main.go:640-673` | A single-lane worker lets a 15-minute video job block pushes and sweeps |
| F-22 | P3 | Jobs | CODE-SUPPORTED RISK | `internal/jobs/worker.go:221-232`, infra `charts/norviq-app/templates/deployment.yaml:13-17` | Periodic enqueues have no dedupe; the RollingUpdate `maxSurge: 1` briefly runs two workers |
| F-23 | P3 | AI / UX | CONFIRMED DEFECT | `internal/coach/service.go:692,924-958` | The SSE `done` event waits for title generation; post-save steps share one 15 s budget |
| F-24 | P3 | AI / billing | CONFIRMED DEFECT | `internal/ai/anthropic/stream.go:66-77,135-142`, `internal/ai/openaicompat/client.go` | A mid-stream timeout ends "cleanly": truncated text is saved and usage goes unbilled |
| F-25 | P3 | AI | CODE-SUPPORTED RISK | `internal/ai/gemini/gemini.go:90-150` | Gemini errors arrive as the first chunk, so the chain never fails over |
| F-26 | P3 | Waste | CONFIRMED DEFECT | `internal/insights/digest_sweep.go:42,211,222` | The digest is fully computed and rendered every hour before the "already sent" check |
| F-27 | P3 | HTTP | CODE-SUPPORTED RISK | `internal/shared/echox/echox.go:324-328` | There is no Read/WriteTimeout, including on small JSON routes |
| F-28 | P3 | Concurrency | CONFIRMED DEFECT (logic race) | `internal/settings/api.go:398`, `internal/aicreds/db/queries.sql:4-11,35` | A slow tools probe can overwrite a newer credential's `supports_tools` |
| F-29 | P3 | Crews | CODE-SUPPORTED RISK | `internal/crews/stream.go:67,89-95`, `internal/crews/api.go:274` | A removed member's SSE can stay open; one DB query per subscriber per event |
| F-30 | P3 | Database | Mixed | see §3.30 | Smaller query issues: N+1 meal ingredients, a 90-day fetch for one value, contact-hash scan, keyset generic plan, unused index |
| F-31 | P3 | Tooling | CONFIRMED DEFECT | `Makefile:12-25`, `internal/auth/webauthn_options_test.go:14` | Makefile targets reference a non-existent `./cmd/aaru`; there is one lint error; no `govulncheck` |
| F-32 | P3 | Ops / memory | MEASURE FIRST | infra `apps/khepri/{web,worker}/values-production.yaml` | `GOMEMLIMIT` is not set against the 768 Mi and 1536 Mi container limits |

---

## 3. Findings in detail

### F-01 — P1 — CONFIRMED DEFECT — Telegram updates handled out of order are silently dropped

*Found independently by two reviewers and re-verified.*

**Path**

1. The webhook or the poller (`poller.go:111-119`) receives an update.
2. `bridge.go:88-92` runs `go func() { b.answer(detached, in, callbackID) }()`, one goroutine per update.
3. `answer` makes network calls before claiming: `AnswerCallback`, and `fillAttachment`, which downloads the photo or document.
4. `messaging.Service.Handle` then calls `ClaimUpdate`:

```sql
UPDATE messaging_links SET last_update_id = $3, ...
WHERE platform = $1 AND external_id = $2
  AND ($3 > last_update_id OR account_id IS DISTINCT FROM $4)
```

If the claim matches no row but the link exists, `service.go:240-244` logs `"messaging ignored a redelivered update"` and returns `Silent: true`.

**Failure scenario**

1. A user sends a photo (update N, slow download), then "what do you think?" (N+1, no download).
2. N+1 claims first and moves the watermark to N+1.
3. N fails the `>` check and is discarded with no reply, recorded only as an INFO log line.
4. Photo albums trigger this every time, because each photo is its own update.

Separately, two turns for one chat run concurrently on the same conversation.

**Proposal**

- Replace the watermark with a seen-set: `messaging_seen_updates(account_id, update_id) PRIMARY KEY`, written with `INSERT … ON CONFLICT DO NOTHING RETURNING`, and purged after 48 h (Telegram retains undelivered updates for 24 h).
- Serialise turns per `ExternalID` in the bridge, using a keyed queue with one drain goroutine per active chat. This also bounds the goroutine count (see F-08).

Serialising alone is not enough, because concurrent webhook POSTs are not ordered.

**Tests**

- A service test: `Handle(update 11)` then `Handle(update 10)` on the same link. Both must reach the coach. This fails today.
- A bridge test: the download for N sleeps 200 ms, and two `sendMessage` calls are expected.
- An atomic in-flight counter per chat must never exceed 1.

**Rollback:** the migration is additive. Keep the old column until the new path is proven.

---

### F-02 — P1 — CONFIRMED DEFECT (source-derived, not reproduced) — a job that times out loops forever

**Evidence (`internal/jobs/worker.go`)**

```go
runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobTimeout) // 15m
...
err = runSafely(runCtx, handler, job.Payload)
...
return true, w.queue.Complete(runCtx, job.ID)        // :187
return true, w.queue.Fail(runCtx, job.ID, err.Error()) // :192
return true, w.queue.Retry(runCtx, job.ID, retryAt, …) // :198
```

**Failure scenario**

1. A handler that honours its context returns `context.DeadlineExceeded` at 15 minutes. `runCtx` has expired by then, so the `Retry` or `Fail` UPDATE fails immediately with a context error. The job stays `running`.
2. After 30 minutes, `ReleaseStaleJobs` (`queries.sql:76-79`) sets it back to `pending` without checking `attempts < max_attempts`.
3. `ClaimJob` increments `attempts` (4, 5, …). The job runs for another 15 minutes and times out again, and `Fail(runCtx)` fails again.

Result: a job whose input always times out (a long video, or a hung provider) consumes about 15 minutes of the **only** worker lane every ~45 minutes, indefinitely. Each run makes provider calls. The `north_job_runs_total{outcome!="success"}` alert will fire, but the loop never ends on its own.

Lesser version of the same bug: a handler that succeeds just before the deadline loses its `Complete`, and the job runs again.

**Proposal**

- Do the bookkeeping on its own short context:

  ```go
  bkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
  defer cancel()
  ```

- Add `AND attempts < max_attempts` to `ReleaseStaleJobs`, plus a sibling statement that marks over-attempted stale jobs `failed` with `last_error = 'abandoned after max attempts'`.
- Log the error when bookkeeping fails. It is logged today, but only as a generic "worker loop error".

**Tests**

- A worker test with `jobTimeout` injectable (for example 50 ms) and a handler that blocks on `<-ctx.Done()`. Assert that the row ends up `pending` with `run_after` in the future (attempt < max), or `failed` (attempt = max).
- A testdb test: a stale `running` row with `attempts = max_attempts` is failed, not requeued.

---

### F-03 — P1 — CONFIRMED DEFECT (source) — re-indexing an edited document fails

**Evidence**

- `ChunkID` hashes `(documentID, ordinal, contentSHA256)` (`ids.go:26`). An edited chunk at an existing ordinal therefore gets a **new** `chunk_id`.
- `UpsertChunk` is `ON CONFLICT (chunk_id) DO NOTHING`, but the table also has `UNIQUE (document_id, ordinal)` (`migrations/20260808134439_create_documents_and_chunks.sql:103`). No later migration changes it.
- `ReplaceChunks` upserts first and deletes stale chunks afterwards, with no transaction (`repository.go:116-151`).

**Failure scenario:** a vault file edit (`vault_sync.go:76-87` reindexes in place under the same document id), or a change to the chunker fingerprint, raises SQLSTATE 23505 on the first changed ordinal. The document is marked failed and keeps its stale chunks.

`TestChunkIDsSurviveAnEdit` (`index_test.go:367`) never changes the content, so it does not catch this.

**Proposal:** in one transaction, run `DeleteChunksNotIn(keep)` first, then the inserts. Under MVCC, readers keep seeing the old chunks until commit, which preserves the property the "upsert first" comment protects. Alternative: make the constraint `DEFERRABLE INITIALLY DEFERRED` and wrap the replace in a transaction.

**Tests:** index a note, edit a middle section, reindex. Expect status `ready` and unchanged chunk ids preserved.

**Production check:**

```sql
SELECT count(*) FROM documents
WHERE status = 'failed' AND parse_error LIKE '%could not store%';
```

---

### F-04 — P1 — CONFIRMED DEFECT — Anthropic outages don't fail over

**Evidence (`internal/ai/anthropic/errors.go`)**

```go
if !errors.As(err, &apiErr) { return fmt.Errorf("anthropic: %w", err) } // :30 no sentinel
...
case managed && status >= 400: ... ErrUnavailable                       // :54
default: return fmt.Errorf("anthropic returned %d: %s", status, detail)  // :57 no sentinel
```

Two error shapes miss the failover sentinel:

- **Transport errors** (DNS, reset, TLS, EOF) are not `*sdk.Error`.
- **In-stream errors.** An `event: error` with `overloaded_error` inside an HTTP 200 stream surfaces from the SDK with `StatusCode == 200`, so it falls to `default`.

In both cases `ai.Failover(err)` is false and `Runner.Run` stops at Anthropic.

**Impact:** with `ANTHROPIC_FIRST=true` in production, an Anthropic overload or network blip before the first token gives the user "coach failed" even though the rest of the chain is healthy. The tests cover only HTTP 529 (`stream_test.go:157`).

**Proposal:**

- Map a non-API error to `ErrUnavailable` unless it is `context.Canceled` or `context.DeadlineExceeded`, as openaicompat `postTo` already does.
- Map error *types* `overloaded_error`, `api_error`, and `rate_limit_error` to `ErrUnavailable` regardless of status.

**Tests:**

- A stub returning `200` plus an `event: error` overloaded body: `Failover(err) == true`.
- A stub that closes the connection mid-headers: same result.
- A Runner test: an Anthropic stub that fails this way, followed by a fake provider, returns the fake's reply.

---

### F-05 — P2 — CODE-SUPPORTED RISK — `jobs` grows forever; the pending check is unindexed

**Evidence**

- The only index is `jobs_claimable_idx (status, run_after) WHERE status='pending'`.
- `HasPendingJobForUser` filters `kind`, `status IN ('pending','running')`, and `(payload->>'user_id')::uuid`, so it cannot use that index.
- `ReleaseStaleJobs` (`status='running'`) cannot use it either.
- The only `DELETE FROM jobs` is in account erasure (`internal/account/db/queries.sql:25`).

**Growth:** about 34 periodic jobs per hour (`cmd/worker/main.go:640-673`), plus one job per crew message, plus per-user embed, sync, extract, and summarize jobs. Every row lives forever.

**Callers:**

- Request path: `fitness/strava/service.go:128`, the Strava status shown on the fitness hub.
- Once per candidate user in four sweeps: `strava/sweep.go:79`, `documents/embed_sweep.go:42` (every 15 minutes), `memories/extract_sweep.go:89`, `conversations/summarize_sweep.go:64`.

**Amplifier:** one bad chunk fails the whole embed job (`embed_job.go:62-122`) and leaves it `failed`. `HasPendingJobForUser` ignores `failed`, so the 15-minute sweep re-enqueues the job forever, with up to 3 provider calls per attempt.

**Proposal**

Add the index in its own migration. It needs `-- +goose NO TRANSACTION` because of `CONCURRENTLY`:

```sql
CREATE INDEX CONCURRENTLY jobs_active_kind_user_idx
  ON jobs (kind, (payload->>'user_id'))
  WHERE status IN ('pending','running');
```

Then:

- Compare `payload->>'user_id' = $user::text`, without the `::uuid` cast on the column, so the index is usable.
- Add retention: a daily batched delete of `done` rows older than 14 days. Keep `failed` rows longer.
- Make the embed sweep skip users who have a recent `failed` embed job, and make the embedder skip or poison individual bad chunks.

**Write amplification:** small. Status changes are already non-HOT, and only pending/running versions enter the new index.

**Measure first:**

```sql
SELECT status, count(*) FROM jobs GROUP BY 1;
SELECT pg_size_pretty(pg_total_relation_size('jobs'));
```

Then `EXPLAIN (ANALYZE, BUFFERS)` on a staging copy, before and after.

---

### F-06 — P2 — CODE-SUPPORTED RISK — health ingest makes 50k round trips in one transaction

**Evidence:** `health/repository.go:41-67` loops `UpsertHealthMetric … RETURNING *` (the result is discarded) once per reading inside one transaction. `maxReadings = 50_000`. The call is on the request path (`handler.go:151`).

**Estimate (not measured):** at 0.3–0.5 ms per round trip, a full batch holds 1 of 10 pool connections and its row locks for 15–40 s. Two overlapping syncs with different orderings can deadlock (40P01).

**Proposal**

1. Dedupe by `(source, metric, started_at)` in Go. Otherwise a multi-row upsert raises "cannot affect row a second time".
2. Sort by `(metric, started_at)` so locks are taken in a deterministic order.
3. Write in 2–5k-row statements via `unnest(...)` or `pgx.Batch`, without `RETURNING`.

**Verify:** a 50k-reading benchmark on testdb (transaction duration), and `pg_stat_activity` during a real sync.

---

### F-07 — P2 — CODE-SUPPORTED RISK — rollouts kill detached work

*Merged from four reviewers.*

**Evidence**

- **Web.** `echox.Serve` has a `GracefulTimeout` of 30 s, which equals the Kubernetes default grace period. The infra chart sets neither `terminationGracePeriodSeconds` nor a preStop hook. Then `defer pool.Close()` runs and the process exits.
- **Coach generation** is detached for up to 5 minutes (`coach/service.go:365,389`).
- **Telegram turns** are detached for up to 6 minutes (`bridge.go:88`).
- **The `runBackground` goroutine** is never joined (`server.go:131`).
- **Worker.** A job runs on `context.WithoutCancel` for up to 15 minutes (`worker.go:167`), but SIGKILL arrives at 30 s. The killed job stays `running` until `staleAfter` (30 minutes) releases it.

**Impact, on every deploy of the single replica:**

- In-flight replies are lost. The message is stored but never answered, which contradicts the comments at `bridge.go:59-66` ("the reply is still persisted") and at `coach/service.go:312-318`.
- PostHog may not flush.
- Interrupted jobs are delayed by about 30 minutes.

**Proposal**

1. Add an `internal/shared/lifecycle` helper that tracks detached goroutines (it also does the F-08 recovery).
2. After `Serve` returns, drain the tracker with a bound below the grace period. Only then close the pool and PostHog.
3. In infra: preStop `sleep 5`, `terminationGracePeriodSeconds: 90–120` for web, and longer for the worker, or make the worker cancel the job on SIGTERM and immediately `Retry` it.
4. Worker: on shutdown, give the current job a bounded window, then requeue it explicitly (`Retry(now)`) rather than waiting for the stale sweep.

**Test:** a cmd-level test starts a reply against a stub LLM that streams for 10 s, cancels the root context, and asserts the assistant row exists before `run` returns within budget.

---

### F-08 — P2 — CODE-SUPPORTED RISK — panics outside the handler goroutine crash the pod

**Evidence**

- `echomw.Recover()` (`echox.go:69`) covers only the handler goroutine.
- `golang.org/x/sync/errgroup.Go` does not recover by design.
- There are about 150 `g.Go` sites on request paths: day, dashboard, insights, stats, recaps, lifts, crews.
- Fire-and-forget goroutines at `bridge.go:81,89,138` and `settings/api.go:398` have no `recover` either.

**Impact:** an index-out-of-range in any loader, for example `scores[w][d]` in `insights/areas.go`, kills the only web pod. That ends every SSE stream and every in-flight turn.

**Proposal:** a helper that turns a panic into an error carrying the stack:

```go
func Safe(name string, f func() error) func() error
```

Use it for every `g.Go` body, and use the F-07 lifecycle helper for fire-and-forget work. A `go vet`-style grep check in CI keeps new bare `g.Go(func` from creeping in.

**Test:** an errgroup with a panicking wrapped func returns an error, and the test process survives.

---

### F-09 — P2 — CONFIRMED DEFECT (race reproduced) — `font.Face` is shared across goroutines

**Evidence**

- `rasterize/text.go:35-36,82` caches `font.Face` by size in `faceCache`.
- Faces are used outside `faceMu` in `DrawString` (`:105`) and `MeasureString` (`:124`).
- `x/image` documents that `opentype.Face` is not safe for concurrent use.
- A reviewer reproduced the race by running `-race` over an overlay test: 8 goroutines × 5 `RenderCard` calls produced a `DATA RACE` in `sfnt.(*Font).LoadGlyph`. The overlay is in the session scratchpad; nothing was written into the repo.

**Path:** the Telegram `/stats` command → `insights.Digest` → `RenderCard`, running in the unbounded, unrecovered per-update goroutines (F-01, F-08). The result is garbled output or a process-killing panic.

**Proposal:** cache only the `*opentype.Font`, which is safe for concurrent use, and build faces per `RenderCard` call.

**Test:** check the concurrent `RenderCard` test into the repo and run it under `-race`.

---

### F-10 — P2 — CONFIRMED DEFECT — `/resume` has no state guard

**Evidence:** `coach.Service.Resume` (`confirm.go:250-298`) builds context and starts a full tool-enabled model turn without checking that the last message carries tool results. It also has no per-conversation guard; `ResolvePending` has one in `resolving`. The function is documented as spending no quota.

**Impact:**

- Repeated or concurrent POSTs each append a model reply and run a free model turn. This is a cost and abuse concern; it is flagged for the security review and not assessed here.
- If the last message is a model text turn, the request ends with an assistant turn. That is assistant prefill, and **Haiku 5.5 returns 400** on it (see the infra Haiku-trial doc). The managed client then fails over to another provider, which "continues" the old reply.
- If the last turn is a tool call that is still pending, every provider returns 400.

**Proposal:** load `latestTurn`, require that the last message has `ToolResults`, and return `ErrConflict` otherwise. Take the same `resolving`-style guard.

**Test:** `Resume` after a completed text reply returns `ErrConflict` with zero `Chat` calls. Two concurrent resumes produce exactly one reply.

---

### F-11 — P2 — CONFIRMED DEFECT — threads over 200 messages

**Evidence**

- `ListMessages` is `ORDER BY created_at LIMIT $2`, oldest first. `History` calls it with 200.
- `coach/api.go:265` returns it as the thread view.
- `coach/api.go:284` treats `history[len-1]` as the latest message.
- `coach/api.go:589` reloads all 200 full rows, including `provider_state` blobs, after **every** reply, just to find `done.MessageID`.
- `latestTurn` (`confirm.go:93-97`) already uses `Recent`, and its comment describes exactly this bug.

**Impact:** long threads, especially linked Telegram threads, are missing their newest turns. The "helpful" rating attaches to an old message. Every reply pays a 200-row read.

**Proposal:** have the pump return the saved message id, or use `Recent(…, latestTurnWindow)`. Display the thread newest-first with a `before` cursor.

**Test:** seed 205 messages, then assert that the last message is shown and that `done.MessageID` is the new row.

---

### F-12 — P2 — CODE-SUPPORTED RISK — retry stacking and no first-token budget

**Retry stacking**

- The managed Anthropic client sets no `option.WithMaxRetries`. SDK v1.84.0 defaults to 2 retries on 408, 409, 429, and 5xx, and honours `Retry-After` with no cap.
- On a 429 or 529 storm against the $20 trial workspace, each turn makes 3 attempts and may sleep tens of seconds before the chain moves on.
- Title, outreach, and summary calls pay the same cost.

**No first-token budget**

- One 5-minute `genCtx` covers every attempt.
- The managed client uses `http.DefaultClient`, which has no `ResponseHeaderTimeout`.
- `Failover` refuses `DeadlineExceeded`, so a provider that stalls consumes the whole budget and the next provider is never tried.
- SSE headers are written only after `SendIncoming` returns (`coach/api.go:561`), so the client sees no bytes at all until then.

**Proposal**

- `WithMaxRetries(0 or 1)` for managed (chained) clients only. BYOK clients keep the SDK defaults.
- A per-attempt first-token budget of 30–45 s, implemented with `time.AfterFunc(cancel)` and stopped once streaming starts. Classify "budget hit, parent alive" as failover.
- Write SSE headers and a `: ping` comment before starting generation.
- Optionally, a short per-provider cooldown after `ErrUnavailable` or `ErrPaymentRequired`.

**Test:**

- A stub returning 429 with `Retry-After: 30` is hit exactly once, and the call returns `ErrUnavailable` in under 1 s.
- A stalling first stub plus a good second stub: the reply comes from the second within budget plus epsilon.

---

### F-13 — P2 — CONFIRMED DEFECT — spend attribution gaps; synchronous meter write

**Attribution gaps**

- `coach/service.go:515` attributes `genCtx` only inside `startChat`, as a local reassignment.
- `pump` uses the unattributed context for follow-up tool rounds (`:878`). Those rounds carry the largest prompts, and they are recorded with `UserID=nil` and surface "unknown".
- `spend.SurfaceSummary` is never set anywhere.

**Synchronous meter write**

- `ai/meter.go:125-147` records spend inline before `close(out)`.
- It uses `context.WithoutCancel`, which also strips the deadline, so a saturated pool can stall the end of the stream indefinitely.
- `Generate` records on the caller's own context, so a cancel right after the call loses the spend row.

Both contradict the `Meter` contract ("Record must not block").

**Proposal**

- Attribute once in `SendIncoming`, `BeginReflection`, and `Resume`, before both `startChat` and `pump`.
- Wrap `HandleSummarizeJob` with `aiattr.WithUser(…, SurfaceSummary)`.
- Record spend after `close(out)` with `WithTimeout(WithoutCancel(ctx), 2*time.Second)`.

**Test:** a two-round tool turn produces two records, both carrying the user and `SurfaceCoach`. With a recorder that blocks for 5 s, the channel still closes in under 100 ms.

---

### F-14 — P2 — MEASURE FIRST — request fan-out vs. `MaxConns=10`

**Evidence**

- A `/day` snapshot starts about 20 `g.Go`, plus two nested `Timeline` calls at 12 each, for roughly 40 concurrent acquires.
- The dashboard fans out about 18, insights about 18, and the coach `ContextBuilder` about 18 per reply.
- Only `insights/areas.go:58` and `crews/service.go:365` call `SetLimit`.
- `pool.Stat()` is not exported.

There is no deadlock, because nothing holds one connection while waiting for another. But one page load can take every connection, and ingest, Telegram, crew writes, and `/healthz` then queue behind it. A `/healthz` that queues long enough to fail can combine with F-15 into restarts.

**Proposal**

1. **First**, export `AcquireDuration`, `EmptyAcquireCount`, `AcquiredConns`, and `MaxConns` to `internal/shared/metrics`.
2. Only if waits show up: `g.SetLimit(4–6)` on the large groups, and collapse per-metric query loops into `metric = ANY($2)`.
3. Size `MaxConns` against Postgres `max_connections` across the web, worker, and mcp-server pools plus the LISTEN connection.

---

### F-15 — P2 — CODE-SUPPORTED RISK — probes

**Web.** Liveness is `/healthz`, which pings Postgres. A Postgres blip longer than about 60 s (3 failures × 20 s) restarts the web pod. That cannot help, and the restart drops every SSE stream. Liveness should be a process-only `/livez`; keep the DB ping on readiness.

**Worker.** Liveness hits the metrics handler, which answers on any path regardless of the loop's health. A worker stuck in a handler that ignores its context, or spinning on a dead pool, still reports alive. `north_worker_heartbeat_timestamp_seconds` exists, but no alert rule uses it (`platform/infra/observability/alerts/platform_rules.py`).

**Proposal:**

- Add `/livez` on the worker's listener that fails when the heartbeat is older than `jobTimeout + 2×pollInterval`, or at minimum add an alert on `time() - north_worker_heartbeat_timestamp_seconds > 1200`.
- The worker heartbeat updates only on a successful `Claim`, which pauses during a long job. Size the threshold above `jobTimeout`, or emit the heartbeat from a ticker as well.

---

### F-16 — P2 — CODE-SUPPORTED RISK — video upload memory

**Evidence**

- `media/api.go:113-114` calls `r.FormFile("video")` without a prior `ParseMultipartForm`, so it takes net/http's default 32 MiB in-memory part buffer. The buffer can grow to about 64 MiB capacity before spilling to disk.
- `transfermanager` then adds 5 × 8 MiB part buffers.
- Every other multipart handler passes 1–4 MiB explicitly.

**Estimate:** 70–100 MiB of transient heap per concurrent upload in a 768 Mi pod. The upload quota is per user per hour, so there is no global concurrency cap.

**Proposal:**

- Call `ParseMultipartForm(4<<20)` before `FormFile`. Better: use `r.MultipartReader()` and stream the part straight to S3.
- Set `transfermanager` `Concurrency: 2`.
- Add a small global semaphore on the upload group.

**Verify:** a benchmark posting a 90 MiB body, checking `HeapInuse`, then measured under 5 concurrent uploads.

---

### F-17 — P2 — CODE-SUPPORTED RISK — no request limiter on the bearer API

**Evidence**

- `cmd/api/api.go:105,107` mounts `auth.Bearer` with no limiter, so every request, including garbage tokens, costs one `GetSessionUser` query.
- `internal/shared/ratelimit/ratelimit.go:1-7` describes a per-key bound that "every bearer-authenticated surface applies", but only MCP actually applies it (`mcpserver/http.go:274-279`).

**Impact:** a retry-looping client can starve the 10-connection pool.

**Proposal:** a per-IP limiter before `Bearer`, and a per-account limiter after it, both returning 429 with `Retry-After`. Mirror `throttleAnonymous` from MCP.

The availability side is in scope here. The abuse side goes to the security review.

---

### F-18 — P2 — CONFIRMED DEFECT (index unusable) — conversation sweeps

**Extraction sweep**

- `ConversationsAwaitingExtraction` filters `(memories_extracted_at IS NULL OR memories_extracted_at < updated_at)`.
- Its index, `conversations_extraction_sweep_idx`, is partial `WHERE memories_extracted_at IS NULL`. The query predicate does not imply the index predicate, so the planner can never use it.
- Idle threads below `min_messages=2` never leave the candidate set.

**Summary sweep** (`queries.sql:131-148`): no pruning predicate at all. It runs a correlated `count(*)` over every conversation every hour.

**Proposal**

- Rebuild the extraction index with the query's exact predicate:

  ```sql
  CREATE INDEX CONCURRENTLY … ON conversations (updated_at)
  WHERE memories_extracted_at IS NULL OR memories_extracted_at < updated_at;
  ```

- Mark short idle threads as handled so they leave the set.
- For the summary sweep, add `AND c.updated_at > coalesce(c.context_summary_through, '-infinity')`.

**Verify:** `EXPLAIN (ANALYZE, BUFFERS)` before and after, on staging.

---

### F-19 — P2 — CODE-SUPPORTED RISK — orphaned `tool_result` at the window edge

**Evidence:** `ContextBuilder.Build` uses `Recent(…, 20)`, a plain `LIMIT`. `ToAIMessages` emits a `ToolResults` row even when its `ToolCalls` row fell outside the window.

**Impact:** Anthropic rejects the orphan with a 400 (the managed client then fails over). OpenAI-dialect providers reject `role: "tool"` without a preceding call, and that 400 does not fail over. The thread stays broken until the window slides past the cut.

This is documented provider behaviour; it was not reproduced here.

**Proposal:** trim leading orphan `ToolResults` rows, and calls without results, until the first plain user or model message.

**Test:** a `ToAIMessages` unit test plus a `toMessages` round trip.

---

### F-20 — P2 — MEASURE FIRST — vector search recall for small tenants

**Evidence:** `SearchChunksByVector` filters by `user_id` and `model`, then orders by `<=>`. There is one global HNSW index plus a btree on `user_id`.

**Risk:** if the planner picks HNSW, pgvector post-filters within `ef_search` (default 40) candidates, so a small user can get zero hits. The caller silently falls back to full-text search, so nobody would notice. The pgvector image is unpinned.

**Measure:**

1. Check `extversion` in production.
2. Run `EXPLAIN ANALYZE` for a small and a large user.
3. Compare recall against a run with `SET enable_indexscan = off`.

**Options:** add a `(user_id, model)` btree and rely on exact scans, since per-user corpora are small, and consider dropping HNSW, which is expensive to write at 1024 dimensions. Or, on pgvector ≥ 0.8, use `SET LOCAL hnsw.iterative_scan = relaxed_order`.

---

### F-21 — P3 — MEASURE FIRST — single-lane worker head-of-line blocking

The worker is single-threaded by design (`worker.go:79-81`), and the design is well reasoned. But a 15-minute form-video analysis, or the F-02 loop, delays `crew_message_push`, which is user-visible, along with every sweep. The hourly `nudges.Evaluate` sweep runs 15–25 queries per onboarded user, serially.

**Measure:** `north_job_duration_seconds` by kind, plus queue wait (`claimed_at - run_after`; add a metric for it).

**If needed:** a second lane, meaning a second `Worker.Run` loop in the same process restricted to interactive kinds (`ClaimJob … AND kind = ANY($1)`). This keeps one process and one set of periodic schedulers, so it does not conflict with the "exactly one replica" rule.

---

### F-22 — P3 — CODE-SUPPORTED RISK — periodic duplication

- `runPeriodic` enqueues unconditionally. If the worker lane is stalled, sweeps such as `sweep_watches` every 15 minutes pile up as duplicates.
- The chart's RollingUpdate (`maxSurge: 1`, `maxUnavailable: 0`) briefly runs two worker pods on every rollout. That contradicts the "DO NOT RAISE" invariant in `values-production.yaml`. The sweeps are idempotent, so the effect is duplicate work, not corruption.

**Proposal:**

- Enqueue a periodic only if no `pending` job of that kind exists. A unique partial index `ON jobs (kind) WHERE status='pending' AND payload = '{}'` plus `ON CONFLICT DO NOTHING` makes this atomic.
- Set `strategy: Recreate` for the worker release.

---

### F-23 — P3 — CONFIRMED DEFECT — `done` waits for title generation

`close(out)` is deferred until after `AppendModelMessage`, `titleConversation` (a full LLM call), `enqueueSummary`, `maybeEndReflection`, and `enqueueMemoryExtraction`, all on one 15 s `saveCtx` (`coach/service.go:692,924-958`).

**Impact:** an extra LLM round trip on every turn of an untitled thread. If the title call consumes the whole budget, the steps after it fail silently.

**Proposal:** close or terminate the stream right after the save, then run the follow-ups each on its own detached context with a timeout, or enqueue the title as a job.

---

### F-24 — P3 — CONFIRMED DEFECT — mid-stream timeout looks like a normal completion

When `genCtx` expires mid-stream, `send(ctx, out, {Err})` selects on the context that is already done, so the error is usually dropped. The usage chunk is never sent (`anthropic/stream.go:66-77,135-142`; openaicompat has the same pattern).

**Impact:** the truncated text is saved as a finished answer, and the meter records zero usage for the most expensive call.

**Proposal:** send the terminal error and the partial usage unconditionally. Consumers always drain to close, so the send cannot block forever.

---

### F-25 — P3 — CODE-SUPPORTED RISK — Gemini never fails over

`GenerateContentStream` is lazy, so `Chat` always returns `(ch, nil)` and the HTTP error arrives as the first chunk (`gemini.go:90-150`). This is P2 if Gemini sits in a production chain.

**Proposal:** pull the first item with `iter.Pull` inside `Chat`, as the Anthropic adapter does, and return a pre-commit error synchronously.

---

### F-26 — P3 — CONFIRMED DEFECT — digest sweep wasted work

`due()` is true for every hour from 08:00 within the period (`digest_sweep.go:42`). The full `Summary` fan-out and PNG render happen at `:211`, before the `ClaimDigest` check at `:222`.

**Impact:** about 16 full digests per day for each daily-cadence user, 15 of them discarded. Digests are opt-in, which limits the blast radius.

**Proposal:** a cheap "already claimed?" read before computing.

---

### F-27 — P3 — CODE-SUPPORTED RISK — no Read/WriteTimeout

`echox.go:324-328` sets only `ReadHeaderTimeout` (10 s) and `IdleTimeout` (120 s). This is intentional, for uploads and SSE. But small JSON routes are then exposed to slow-body clients on in-cluster and Tailscale paths; Cloudflare buffering covers only the edge.

**Proposal:** server-wide timeouts of about 60 s, extended per route group with `http.NewResponseController(w).SetReadDeadline / SetWriteDeadline` for uploads and the three SSE routes.

---

### F-28 — P3 — CONFIRMED DEFECT (logic race) — tools probe overwrites a newer credential

`settings/api.go:398` spawns `ProbeTools` with a 2-minute budget. `RecordUserAICredentialToolSupport` is `WHERE user_id = $1`, with no version guard. If a user saves provider A and then B, A's slow probe can overwrite B's flag. The upsert also never resets `supports_tools`.

**Proposal:** guard the update with `AND updated_at = $saved_version`, and reset the flag to `NULL` on upsert.

---

### F-29 — P3 — CODE-SUPPORTED RISK — crew live stream

- Membership is checked once, at subscribe time (`crews/api.go:274`).
- `EventRemoved` can be lost in a listener reconnect gap (only `Resync` is sent) or to drop-oldest overflow. The removed user then keeps receiving ids and cursors, not content, until they disconnect.
- `viewerGate` runs one `AuthorBlockedFor` query per subscriber per event, which also makes overflow more likely. This part is MEASURE FIRST.

**Proposal:** re-check membership on `Resync`. Cache per-viewer block lists in the stream goroutine.

---

### F-30 — P3 — smaller database items

| Item | Status | Location | Proposal |
|---|---|---|---|
| Meal-plan ingredients N+1, partly under `LockMealPlan` | CODE-SUPPORTED | `meals/repository_meal_plans.go:470-473`, `meal_plan_changes.go:126-147` | One `meal_id IN (SELECT …)` query, grouped in Go |
| Day view fetches 90 days to read `rows[0]` | CODE-SUPPORTED | `day/service.go:748,779-783` | Use existing `LatestHealthMetric` with a `since` bound |
| Contact matching hashes every user row per request | MEASURE FIRST | `social/db/queries.sql:104-121` | Stored `email_sha256` / `phone_sha256` columns with partial indexes |
| Keyset `(@after = '000…' OR id > @after)` may defeat the PK range under generic plans | MEASURE FIRST | `users/db/queries.sql:71` | `WHERE id > @after` (uuid.Nil is the minimum); check `EXPLAIN (GENERIC_PLAN)` |
| Workout-card queries load other users' rows before the ownership check | CODE-SUPPORTED (defence in depth, for the security review) | `activity/db/queries.sql:109`, `lifts/db/queries.sql:55`, `profiles/service.go:258-263` | Push owner ids into SQL |
| `set_logs_user_exercise_idx` matches no query | MEASURE FIRST | lifts migrations | Drop after `pg_stat_user_indexes.idx_scan = 0` |
| `UsersWithEmbeddingGap` is a global anti-join every 15 minutes | MEASURE FIRST | `documents/db/queries.sql:39` | Check `pg_stat_statements` mean time |

---

### F-31 — P3 — CONFIRMED DEFECT — tooling

- `Makefile` `build`, `run`, and `migrate` target `./cmd/aaru`, which does not exist. The binaries are `cmd/api`, `cmd/worker`, and `cmd/mcp-server`, and the header still says "Aaru API".
- `golangci-lint run ./...` reports one issue: an `errcheck` at `internal/auth/webauthn_options_test.go:14` (`opt(&cco)`).
- `govulncheck` is not in CI (`.github/workflows/ci.yml`) and not installed locally. Add `go run golang.org/x/vuln/cmd/govulncheck@<pinned> ./...` as a CI step.
- `staticcheck` runs only through golangci-lint. That is fine; the standalone `make staticcheck` target needs a binary that is not installed.

---

### F-32 — P3 — MEASURE FIRST — memory limit awareness

The web pod is limited to 768 Mi and the worker to 1536 Mi, and neither sets `GOMEMLIMIT`. The Go runtime does not derive a soft memory limit from cgroups, so the GC can let the heap overshoot the container limit under bursts such as F-16 uploads or video analysis. That ends in an OOMKill rather than more GC work.

**Proposal:** set `GOMEMLIMIT` to about 85–90 % of the limit in the infra values, and watch `go_memstats_*` and `container_memory_working_set_bytes`.

---

## 4. Prioritised roadmap

No P0 items. Each work item states its order, complexity, and risk. Success thresholds are given only where they follow from the defect itself.

### P1 — fix first

| # | Work item | Files | Complexity | Risk | Verification | Rollback | Status |
|---|---|---|---|---|---|---|---|
| 1 | Telegram seen-set and per-chat serialisation (F-01) | `messaging/db/queries.sql`, new migration, `messaging/service.go`, `telegram/bridge.go` | M | Medium (dedupe semantics) | Out-of-order service test; bridge ordering test; no "ignored a redelivered update" log for fresh ids in production | Keep the old column; feature-flag the claim path | **APPROVE** |
| 2 | Job bookkeeping on a fresh context; `ReleaseStale` respects `max_attempts` (F-02) | `jobs/worker.go`, `jobs/db/queries.sql` | S | Low | Injected-timeout worker test; testdb stale/exhausted test; `attempts > max_attempts` count drops to 0 | Revert commit | **APPROVE** |
| 3 | `ReplaceChunks` delete-then-insert in one transaction (F-03) | `documents/repository.go` | S | Low | Edit-and-reindex integration test; production count of failed "could not store" docs → 0 | Revert commit | **APPROVE** |
| 4 | Anthropic error classification for transport and in-stream errors (F-04) | `ai/anthropic/errors.go`, tests | S | Low (failover is already the designed path) | Stub tests listed in F-04 | Revert commit | **APPROVE** |

### P2 — next

Suggested order: 5 → 6 → 7 → 8, then the rest in any order.

| # | Work item | Depends on | Status |
|---|---|---|---|
| 5 | `internal/shared/lifecycle`: recover + track + bounded drain; wrap `g.Go` and fire-and-forget goroutines (F-07, F-08) | — | **APPROVE** |
| 6 | Infra: preStop sleep, `terminationGracePeriodSeconds`, web `/livez`, worker heartbeat liveness or alert, worker `strategy: Recreate` (F-07, F-15, F-22) | 5 (for drain) | **APPROVE** (infra repo PR) |
| 7 | Rasterizer: cache fonts, not faces; add the `-race` test (F-09) | — | **APPROVE** |
| 8 | `jobs` index, retention, embed re-enqueue guard (F-05) | Measure table size first | **APPROVE** index and query; **MEASURE FIRST** retention window |
| 9 | `/resume` state and concurrency guard (F-10) | — | **APPROVE** |
| 10 | Return the saved message id from the pump; thread pagination (F-11) | — | **APPROVE** |
| 11 | Managed-client `MaxRetries(0–1)`, first-token budget, early SSE headers (F-12) | 4 | **REVISE** if the budget value is not agreed. 30–45 s is a starting point to validate against observed time-to-first-token. |
| 12 | Spend attribution and async bounded record (F-13) | — | **APPROVE** |
| 13 | Export pgxpool stats; then decide on `SetLimit` / `MaxConns` (F-14) | — | **APPROVE** metrics; **MEASURE FIRST** limits |
| 14 | Health ingest batching (F-06) | — | **MEASURE FIRST** (real payload sizes), then **APPROVE** |
| 15 | Upload streaming and memory cap (F-16), `GOMEMLIMIT` (F-32) | — | **APPROVE** the `ParseMultipartForm` one-liner; **MEASURE FIRST** streaming rewrite |
| 16 | Bearer route limiter (F-17) | — | **APPROVE** |
| 17 | Conversation sweep indexes and predicates (F-18) | `EXPLAIN` on staging | **MEASURE FIRST** |
| 18 | Trim orphan tool rows at the window edge (F-19) | — | **APPROVE** |
| 19 | Vector search recall check (F-20) | Production `extversion`, `EXPLAIN` | **MEASURE FIRST** |

### P3 — opportunistic

F-21 (second worker lane: **MEASURE FIRST**), F-22 (periodic dedupe: **APPROVE**), F-23 to F-26 (**APPROVE**, small), F-27 (**APPROVE** with route-group deadline middleware), F-28 and F-29 (**APPROVE**), F-30 (per row), and F-31 (**APPROVE**: fix the Makefile, the lint error, and add govulncheck).

### Explicitly not recommended

- **A goroutine pool inside the worker** to "speed up jobs." **REJECT** for now. The single-lane design is deliberate and correct for crash semantics. If F-21 measurements justify it, add a second, kind-restricted lane instead.
- **Raising worker `replicaCount`.** **REJECT** until there is leader election or advisory-lock-guarded periodics (F-22).
- **A Redis or broker queue.** **REJECT.** The Postgres queue is fine. Its problems (F-02, F-05) are bookkeeping and indexing, not the choice of technology.
- **A blanket `sync.Pool` or pointer-receiver sweep.** **REJECT.** No profile shows allocation pressure.

---

## 5. Evidence log

| Command | Environment | Result |
|---|---|---|
| `git rev-parse HEAD`, `git status --short` | local | `bbb4b7d7…`, clean |
| `go version` | local | go1.27.3 darwin/arm64 |
| `go vet ./...` | local | exit 0, no findings. **Note:** this populated the module cache with a few go.sum-pinned modules (`go: downloading …`); nothing in the repo changed. |
| `env -u DATABASE_URL -u TEST_DATABASE_URL go test -race -count=1 ./...` | local, no Postgres, no API keys in the environment | exit 0. 148 packages ok, 0 FAIL, no `DATA RACE`. All testdb-backed tests **skipped**. |
| `golangci-lint run ./...` (repo `.golangci.yml`) | local | 1 issue: errcheck at `internal/auth/webauthn_options_test.go:14` |
| `go test -race -run TestHub ./internal/crews/live/` and `go test -race ./internal/messaging/telegram/` | local (reviewer) | ok |
| `go test -race -overlay=<scratchpad overlay> -run TestZZRenderCardConcurrently ./internal/shared/rasterize/` | local (reviewer); the overlay added a virtual test file only | **DATA RACE** reproduced (F-09) |
| `staticcheck`, `govulncheck` | — | Not installed; not run |
| Infra values and chart read | `platform/infra/apps/khepri/**`, `charts/norviq-app/templates/deployment.yaml`, `observability/alerts/platform_rules.py` | Used for F-07, F-15, F-22, F-32 |

**Method:** the main pass covered entrypoints, the job queue, the worker, the DB pool, migrations, and infra. Four parallel read-only reviewers covered AI and streaming, the HTTP edge and outbound clients, database queries, and goroutines and shared state. The P1 and confirmed claims were re-verified against the source before inclusion: F-01, F-02, F-03, F-04, F-09 (code), and F-10.

### Data needed to close the MEASURE FIRST items

- `SELECT status, count(*) FROM jobs GROUP BY 1`; `pg_total_relation_size('jobs')`.
- `pg_stat_statements` top queries by total time and calls, especially `HasPendingJobForUser`, `ConversationsAwaitingExtraction`, the summary sweep, `UsersWithEmbeddingGap`, and `MatchContactHashes`.
- `EXPLAIN (ANALYZE, BUFFERS)` on a **staging copy only** for those queries and for `SearchChunksByVector` with a small and a large user. Never run `EXPLAIN ANALYZE` against production; it executes the query.
- pgvector `extversion` in production.
- pgxpool stats once exported (F-14).
- `north_job_duration_seconds` by kind, plus a queue-wait metric (F-21).
- Real health-ingest payload sizes (F-06), and container memory during video uploads (F-16, F-32).

### Potential tensions between recommendations

- **F-12 vs. F-04.** Fewer SDK retries means more failover traffic to the chain. That is intended, since the chain exists for this, but watch spend on the fallback providers.
- **F-07 grace period vs. deploy speed.** A 90–120 s grace period makes rollouts slower. Cancelling generation on SIGTERM and saving the partial text is the alternative if that cost is unacceptable.
- **F-14 `SetLimit` vs. page latency.** Limiting fan-out can raise p50 for `/day`. Decide from pool-wait metrics, not up front.

---

## 6. Hand-off to a security review (not assessed here)

These came up during the engineering pass. They are listed so they are not lost. They are **not** security findings.

- `/resume` running free, repeatable model turns (F-10).
- No request limiter on `/api/v1` bearer routes (F-17).
- Web-push endpoint URLs accept any `https://` host, and the server POSTs to them (`internal/push/push.go:76`), which is an SSRF surface.
- Owner filtering applied after the fetch on workout cards (F-30).
- Crew SSE continuing after member removal (F-29).

## 7. Things done well (keep them)

- The Postgres `SKIP LOCKED` queue: transactional `EnqueueTx`, clock-aligned periodics, and panic-to-failed-job conversion.
- pprof and metrics on separate loopback listeners; never on the public router.
- Every outbound HTTP client has a timeout; bodies are closed and `LimitReader`-bounded; APNs and web push prune dead tokens on 410/404.
- Every `pool.Begin` defers `Rollback`; no LLM or network call happens inside a transaction; no `context.Background()` on request paths.
- The crew live hub never blocks on a slow subscriber, ends cleanly on shutdown, and reconnects its LISTEN with backoff and resync.
- The Haiku 5.5 constraint is honoured: no `temperature`, `top_p`, or `top_k` is sent by the Anthropic adapter.
- Client-IP derivation walks `X-Forwarded-For` from the right with explicit trusted proxies.
- Migrations run once, in the PreSync hook, in the cluster (`AUTO_MIGRATE=false`); goose uses a session lock locally.
