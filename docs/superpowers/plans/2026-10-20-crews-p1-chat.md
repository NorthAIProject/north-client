# Crews P1 — Text Chat + Realtime (web + API) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Crewmates chat in their crew's text channels on the web and through the API. Messages arrive live and pushes reach them. Moderation (filter, terms, reports, blocks, ops queue) is in place before any iOS build carries chat.

**Architecture:** Messages live in `crew_messages` and are written by a new `crews.Chat` service. Every write also runs `pg_notify('crew_events', ids-only JSON)` inside its transaction. The web binary holds one dedicated `LISTEN` connection (`internal/crews/live`) that fans events out through an in-process `Hub` to SSE streams. An event is only a signal: clients fetch `messages?after={seq}`, rendered per viewer, so blocks, hides and language are always applied server-side. Pushes go through a worker job.

**Tech Stack:** Go, pgx/v5 (`pgx.Conn.WaitForNotification`), sqlc, goose, templ + templUI, htmx 4.0.0 + its vendored SSE extension (`hx-sse:connect`), Alpine. No new dependencies.

**Spec:** `~/.claude/plans/groups-discord-like-servers-with-parallel-brook.md` (approved 2026-10-20), P1 row plus the Realtime, Moderation, API and UX sections. iOS (P1i) is a separate later plan. P0 (`docs/superpowers/plans/2026-10-20-crews-p0-foundation.md`, PR #137) is the base.

## Global Constraints

- **Workspace:**
  - Work only in worktree `.worktrees/crews-p1`, branch `feat/crews-p1`, stacked on `feat/crews-p0`. Rebase onto main once #137 merges.
  - Every go command: `GOCACHE=$PWD/.gocache`.
  - DB tests: `TEST_DATABASE_URL=postgres://north:north@localhost:5434/north?sslmode=disable`. If refused: `orb start && docker start north-client-postgres-1`.
  - Codegen: sqlc v1.31.1 at `.gocache/bin/sqlc`. `templ generate` before building (generated files are gitignored).
- **Migrations:** filenames are UTC digits. Backfills are idempotent and fast (`AUTO_MIGRATE=false`, PreSync hook).
- **API shape:**
  - API changes are additive. New enums (`kind`, `emoji`, event `type`, `notify`) are `type: string` with the known values in the description, never `enum:`.
  - Never touch `FeedItem.category` or XP kinds.
  - `docs/api/openapi.yaml` must match mounted routes (`TestOpenAPISpecMatchesMountedRoutes`). Run `scripts/sync-openapi.sh --check` if present.
- **Copy and UI:**
  - Every user-facing string goes in en/es/pt_pt/pt_br catalogues (`TestEveryCatalogueCoversEnglish`, `TestUntranslatedStringsAreAccountedFor`).
  - Service-side refusals use `i18n.T(ctx, key)`, like `internal/users/service.go:187`.
  - Use templUI components (`web/shared/ui/`); hand-rolled markup needs a reason in a comment.
  - htmx 4: hx attributes do not inherit, so each element carries its own. Named SSE events become DOM events on the connecting element and are never swapped. Avoid same-ID swaps around templUI selectboxes.
- **Limits** (constants in `internal/crews/crew/crew.go`):
  - message body 1–4000 runes after trim
  - 20 sends per user per minute (in-memory `ratelimit.New(20)`; web is 1 replica)
  - 20 channels per crew
  - 50 pins per channel
  - pages default 50, max 100
  - reaction emoji keys exactly `fire muscle clap heart laugh hundred trophy eyes` (🔥💪👏❤️😂💯🏆👀)
- **Moderation rules:**
  - Auto-hide threshold is 3 distinct reporters, 2 when the crew has ≤4 members.
  - Swearing is allowed; `moderation.Filter` only stops listed slurs and hate.
  - Posting needs `moderation.CanPost` (terms v1 accepted, not suspended).
  - Blocks: messages, reactions and mentions of someone blocked either way are fully hidden from the viewer and never push them.
- **Push rules:**
  - Default `notify='mentions'` covers mentions and replies to you. `all` covers every message. `none` covers nothing. `muted_until > now` silences.
  - Chat pushes ignore server quiet hours (iOS Focus handles it).
  - Author excluded. Body clipped by APNs at 500 runes.
- **Hidden channel:** `#wins` (kind `wins`) is hidden from channel lists until P2. Only `text` channels are listed or postable in P1.
- **Never log** message bodies, tokens or secrets.

## Review Focus

1. **Sending while the live connection is down or reconnecting.** The sender still sees their message (the composer triggers the same fetch path). After the listener reconnects, every open stream gets `resync` and refetches, so nothing is lost and nothing shows twice. *Tasks 5 and 8.*
2. **A blocked pair in one crew.** Neither sees the other's messages, reactions or mentions, and neither gets pushed by the other. A reply quoting a blocked author shows "Message unavailable", not the text. *Tasks 2, 3 and 7.*
3. **A member removed mid-session.** Their open stream gets `removed` and closes, and further fetches return 404. They stop getting pushes. *Tasks 6 and 7.*
4. **Concurrent sends and duplicate submits.** A double-clicked send with the same `client_id` stores one message. `seq` orders messages across concurrent senders, and `after=` never skips one. *Task 2.*
5. **Rollout with open streams.** On SIGTERM, streams end within a second, so `srv.Shutdown` doesn't wait 30s. *Task 5.*

---

### Task 1: Chat schema + system-message backfill

**Files:**
- Create: `migrations/20261021090000_crew_chat.sql`
- Test: `internal/shared/database/crew_chat_migration_test.go`

**Interfaces — Produces (schema):**
- `crew_messages`:
  - `id uuid PK default gen_random_uuid()`
  - `seq bigint GENERATED ALWAYS AS IDENTITY UNIQUE`
  - `crew_id → crews ON DELETE CASCADE`
  - `channel_id → crew_channels ON DELETE CASCADE`
  - `author_id → users ON DELETE CASCADE` (NULL = system)
  - `kind text NOT NULL CHECK (kind IN ('text','system'))`
  - `body text NOT NULL CHECK (char_length(body) <= 4000)`
  - `reply_to_id → crew_messages ON DELETE SET NULL`
  - `mentions uuid[] NOT NULL DEFAULT '{}'`
  - `mention_all boolean NOT NULL DEFAULT false`
  - `client_id uuid`
  - `pinned_at timestamptz`, `pinned_by → users ON DELETE SET NULL`
  - `edited_at timestamptz`
  - `deleted_at timestamptz`, `deleted_by → users ON DELETE SET NULL`
  - `hidden_at timestamptz`
  - `created_at timestamptz NOT NULL DEFAULT now()`
  - `UNIQUE (author_id, client_id)`
  - `CHECK ((kind = 'system') = (author_id IS NULL))`
  - `CHECK (deleted_at IS NULL OR body = '')`
  - Indexes: `(channel_id, seq)` and `(channel_id, pinned_at) WHERE pinned_at IS NOT NULL`.
- `crew_message_reactions (message_id → crew_messages CASCADE, user_id → users CASCADE, emoji text CHECK (emoji IN (<the 8 keys>)), created_at, PRIMARY KEY (message_id, user_id, emoji))`
- `crew_channel_reads (channel_id → crew_channels CASCADE, user_id → users CASCADE, last_read_seq bigint NOT NULL DEFAULT 0, PRIMARY KEY (channel_id, user_id))`
- Backfill: one system message with body `crews.sys.spaces` in each crew's default channel. It is idempotent: insert only where that channel has no message with that body.

- [ ] **Step 1: Write the failing test** `TestCrewChatMigrationBackfillsSystemMessage`.
  - Harness copied from `crew_spaces_migration_test.go`, with its own DB name.
  - `UpTo(20261020130000)`, then insert a user and a crew with the owner as a member and `#general`, then `UpTo(20261021090000)`.
  - Assert exactly one row: `kind='system'`, `author_id IS NULL`, `body='crews.sys.spaces'`, in the default channel.
  - Down one step, Up again: still exactly one.
- [ ] **Step 2: Run it, expect FAIL** (no migration). `go test ./internal/shared/database -run CrewChat`
- [ ] **Step 3: Write the migration**, with a header comment on the ids-only notify contract and why body holds an i18n key for system rows.
- [ ] **Step 4: Run, expect PASS.** Then `go test ./internal/shared/database`.
- [ ] **Step 5: Commit** `Crews chat: messages, reactions and read cursors; one system message per crew`.

### Task 2: Send and list messages

**Files:**
- Modify: `internal/crews/crew/crew.go`, `internal/crews/db/queries.sql` (+ regenerate)
- Create: `internal/crews/chat.go`, `internal/crews/mentions.go`, `internal/crews/live/notify.go`
- Modify: `internal/jobs/jobs.go` (`EnqueueTx`, `KindCrewMessagePush`, `CrewMessagePushPayload`)
- Test: `internal/crews/chat_test.go`, `internal/crews/mentions_test.go`

**Interfaces — Produces:**
- `crew.Message{ID uuid.UUID; Seq int64; ChannelID uuid.UUID; Author *Person; Kind string; Body string; ReplyTo *Quote; Mentions []uuid.UUID; MentionsMe bool; Reactions []Reaction; Pinned, Edited, Deleted bool; CreatedAt time.Time}`
- `crew.Person{ID uuid.UUID; Name, Handle string}`
- `crew.Quote{ID uuid.UUID; AuthorName, Excerpt string; Unavailable bool}`. The excerpt is ≤120 runes. `Unavailable` covers deleted, hidden or blocked.
- `crew.Reaction{Emoji string; Count int; Mine bool}`
- `crew.Page{Messages []Message; HasMore bool}`
- Constants: `MessageKindText = "text"`, `MessageKindSystem = "system"`, `ReactionEmoji` (ordered keys → glyph), `MaxBody = 4000`, `PageSize = 50`, `MaxPage = 100`.
- `live.Event{Type string; CrewID, ChannelID, MessageID, UserID uuid.UUID; Seq int64}` JSON with camelCase keys and `omitempty` on zero ids.
  - Types: `EventMessage = "message"`, `EventUpdate = "update"`, `EventChannels = "channels"`, `EventRemoved = "removed"`, `EventResync = "resync"`.
- `live.Notify(ctx context.Context, tx pgx.Tx, ev Event) error`, which runs `SELECT pg_notify('crew_events', $1)`. Also `live.Channel = "crew_events"`.
- `func (q *Queue) EnqueueTx(ctx context.Context, tx pgx.Tx, kind Kind, payload any) (Job, error)`
- `jobs.CrewMessagePushPayload{UserID uuid.UUID `json:"user_id"` /* author */; MessageID uuid.UUID `json:"message_id"`}`
- Chat service:
  - `type Chat struct` with `func NewChat(pool *pgxpool.Pool, mod *moderation.Service, filter *moderation.Filter, queue *jobs.Queue) *Chat`
  - `func (c *Chat) Send(ctx context.Context, crewID, channelID, authorID uuid.UUID, in SendInput) (Message, error)`, with `SendInput{Body string; ReplyToID *uuid.UUID; ClientID *uuid.UUID}`
  - `func (c *Chat) List(ctx context.Context, crewID, channelID, viewerID uuid.UUID, before, after int64, limit int) (Page, error)`
  - `func (c *Chat) Get(ctx context.Context, crewID, messageID, viewerID uuid.UUID) (Message, error)`
- `func parseMentions(body string) (handles []string, all bool)`. A mention is `@` + `[a-z0-9_]{2,30}`, case-insensitive. `all` is true for `@everyone`. Duplicates are dropped.

**Rules (Send):**
- Not a member, or channel not `text` in this crew → `apperr.ErrNotFound`.
- Body is trimmed; empty or over `MaxBody` → `FieldErrors{"body": i18n "crews.err.body"}`.
- `filter.Check` → `FieldErrors{"body": i18n "crews.err.blocked"}`.
- `mod.CanPost`: `ErrTermsRequired` and `ErrSuspended` are returned as-is, and handlers map them.
- Rate limit exceeded → `FieldErrors{"body": i18n "crews.err.slow"}`.
- Reply target must be in the same channel and visible to the author, else `FieldErrors{"reply": …}`.
- Mentions resolve to members by handle; non-member handles are ignored. `@everyone` only counts from owner/admin.
- A `client_id` duplicate returns the existing message (`ON CONFLICT (author_id, client_id) DO NOTHING`, then select).
- In one tx: insert, then `live.Notify(EventMessage)`, then `queue.EnqueueTx(KindCrewMessagePush)`.

**Rules (List/Get):**
- Members only. Hidden messages are omitted. Messages from users blocked either way are omitted, and their reactions aren't counted.
- Messages the viewer reported are omitted (`mod.ReportedBy`).
- Deleted messages return `Deleted=true`, empty body, `Author` kept.
- `before`/`after` are seq cursors, exclusive; at most one is non-zero. `after` returns ascending. `before` and none return the newest page, ascending, with `HasMore` meaning older messages exist.
- System message bodies are passed through as keys; templates translate them.

- [ ] **Step 1: Write failing tests:**
  - `TestParseMentions`: table with `"@ana hi @Leo"`→`[ana leo]`; `"mail a@b.com"`→none; `"@everyone go"`→all; `"@a"`→none (too short); a dupe `@ana @ana`→one.
  - `TestSendAndList`: two members; Ana sends 3. `List(after=0)` → 3 ascending with seqs increasing. `List(after=seq[0])` → 2. `List(before=seq[2], limit=1)` → `[msg1]`, `HasMore=true`.
  - `TestSendValidates`: empty → body error; 4001 runes → body error; a filtered slur (first term of `moderation.TermsForTest()["en"]`) → body error; a non-member → ErrNotFound; terms not accepted → `moderation.ErrTermsRequired`; the 21st send in a minute → body error.
  - `TestSendIdempotentClientID`: the same `ClientID` twice → one row, same ID.
  - `TestMentionsResolveToMembers`: `@leo` (member) is in `Mentions`, `@zed` (non-member) is not. `@everyone` from a member sets `mention_all=false`; from the owner, true.
  - `TestBlockedAuthorsHidden`: Ana blocks Leo. Leo's message is absent from Ana's list and present in Owner's. Ana's reply quoting Leo's message shows `ReplyTo.Unavailable` for Ana only.
  - `TestSendNotifiesAndQueues`: `LISTEN crew_events` on a separate conn, then Send. Assert one notification whose JSON has `type=message`, the channelId and seq. Assert one `jobs` row of kind `crew_message_push`.
- [ ] **Step 2: Run, expect FAIL** (undefined). `go test ./internal/crews -run 'Mentions|Send|List|Blocked'`
- [ ] **Step 3: Implement.** Queries in `internal/crews/db/queries.sql` are named by verb (`InsertMessage`, `MessagesAfter`, `MessagesBefore`, `MessageByID`, `ReactionsFor`, `MembersByHandle`). Block filtering is done in SQL with `NOT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id=$viewer AND blocked_id=author_id) OR (blocker_id=author_id AND blocked_id=$viewer))`.
- [ ] **Step 4: Run, expect PASS**, then `go test ./internal/crews ./internal/jobs`.
- [ ] **Step 5: Commit** `Crews chat: send and list messages, mentions, blocks hidden both ways, notify in the write tx`.

### Task 3: Edit, delete, reactions, pins, reads, channels, notify settings

**Files:**
- Modify: `internal/crews/chat.go`, `internal/crews/service.go`, `internal/crews/db/queries.sql`
- Test: `internal/crews/chat_actions_test.go`, `internal/crews/channels_test.go`

**Interfaces — Produces** (all members-only, else `ErrNotFound`; each write notifies inside its tx):
- `Edit(ctx, crewID, messageID, actorID uuid.UUID, body string) (Message, error)`: author only and not deleted; same body validation and filter; sets `edited_at`. Notifies `update`.
- `Delete(ctx, crewID, messageID, actorID uuid.UUID) error`: author or manager; sets `deleted_at/by`, `body=''`, clears pin and reactions. Notifies `update`.
- `React(ctx, crewID, messageID, userID uuid.UUID, emoji string, on bool) error`: unknown key → `FieldErrors{"emoji"}`. Idempotent both ways. Notifies `update`.
- `Pin(ctx, crewID, messageID, actorID uuid.UUID, on bool) error`: managers only. The 51st pin → `FieldErrors{"pin"}`. Notifies `update`.
- `Pins(ctx, crewID, channelID, viewerID uuid.UUID) ([]Message, error)`: newest pin first, same visibility as List.
- `MarkRead(ctx, crewID, channelID, userID uuid.UUID, seq int64) error`: monotonic (`GREATEST`); no notify.
- `Unread(ctx, userID uuid.UUID) ([]crew.Unread, error)`, with `crew.Unread{CrewID, ChannelID uuid.UUID; Count, Mentions int}`.
  - Counts visible text messages in text channels with `seq > coalesce(last_read_seq,0)`, `created_at >= joined_at`, author ≠ user, not deleted/hidden/blocked.
  - Mentions count messages where the user is in `mentions`, or `mention_all` is set, or the message replies to one of the user's messages.
- Channels on `Service`:
  - `CreateChannel(ctx, crewID, actorID uuid.UUID, name, topic string) (Channel, error)`: managers only. Name normalised (lower-case, spaces/underscores→`-`, other characters dropped, trimmed of `-`), then must match `^[a-z0-9][a-z0-9-]{0,29}$`, else `FieldErrors{"name"}`. A duplicate → `FieldErrors{"name"}`. The 21st channel → `FieldErrors{"name"}`. Topic ≤120 runes.
  - `UpdateChannel(ctx, crewID, channelID, actorID uuid.UUID, name, topic string) (Channel, error)`: same rules. `general` cannot be renamed.
  - `DeleteChannel(ctx, crewID, channelID, actorID uuid.UUID) error`: managers only; default or `wins` → `FieldErrors{"channel"}`.
  - Each notifies `channels`. `Channels` now lists only `kind='text'`.
- `SetNotify(ctx, crewID, userID uuid.UUID, notify string, mutedUntil *time.Time) error`: notify is one of `all|mentions|none`, else `FieldErrors{"notify"}`.
- `Remove` and `Leave` now notify `removed` with `UserID`. `Create` inserts the `crews.sys.welcome` system message in `#general`.

- [ ] **Step 1: Write failing tests:**
  - `TestEditOwnOnly`
  - `TestDeleteByAuthorOrManager`: a plain member can't delete another's; an admin can; the result shows `Deleted` with an empty body.
  - `TestReactToggleAndBlockedNotCounted`: Leo and Ana react `fire`; Ana's view counts 1 when she has blocked Leo; a `"poop"` key is refused.
  - `TestPinManagersOnly`
  - `TestUnreadAndMentions`:
    - 3 messages from Leo, one `@ana`, then a reply to Ana's own message: Ana's unread is 4, mentions 2.
    - `MarkRead(seq of 2nd)` gives 2; marking a lower seq later does not raise it.
  - `TestChannelsCRUD`:
    - Normalisation `"Leg Day!"`→`leg-day`.
    - A member's create is refused.
    - Deleting `general` is refused.
    - The 21st channel is refused.
    - `#wins` is not in `Channels`.
  - `TestRemoveNotifiesRemoved` (LISTEN as in Task 2).
  - `TestCreatePostsWelcome`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, expect PASS**, then `go test ./internal/crews`.
- [ ] **Step 5: Commit** `Crews chat: edit, delete, reactions, pins, read cursors, channels and notification settings`.

### Task 4: Moderation wiring — hide, report, terms, ops support

**Files:**
- Modify: `internal/moderation/{moderation.go,service.go,db/queries.sql}`
- Create: `internal/crews/moderation.go`, `internal/moderation/{api.go,handler.go}`
- Modify: `internal/config/config.go` (`OperatorUserIDs []uuid.UUID` from `OPERATOR_USER_IDS`, comma-separated, empty allowed)
- Modify: `internal/apns/service.go`, `internal/push/service.go` (`SendThread`)
- Test: `internal/crews/moderation_test.go`, `internal/moderation/service_test.go`, `internal/apns/service_test.go`

**Interfaces — Produces:**
- `func (s *apns.Service) SendThread(ctx, userID uuid.UUID, title, body, href, category, threadID string) (int, error)`. `Send` becomes `SendThread(..., "nudges")`.
- `func (s *push.Service) SendThread(...)` has the same signature; thread is ignored (the service-worker tag is the href).
- Test: `TestAPNsSendThreadSetsThreadID`. A fake `Sender` captures the payload; `aps.thread-id == "crew-x"`; plain `Send` still gives `"nudges"`.
- `Hider` gains `Unhide(ctx, id uuid.UUID) error` and `Excerpt(ctx, id uuid.UUID) (string, error)`.
- `crews.MessageHider` (kind `"crew_message"`):
  - `Hide`/`Unhide` set or clear `hidden_at` and notify `update`.
  - `Threshold` is 2 if the crew has ≤4 members, else 3.
  - `Excerpt` returns ≤200 runes of the body.
- `func (c *Chat) Report(ctx, crewID, messageID, reporterID uuid.UUID, reason, note string) error`: members only, visible message, builds `moderation.Subject{Kind:"crew_message", ID, AuthorID, ScopeID:&crewID}`.
- Moderation service additions:
  - `WithOperators(ids []uuid.UUID, push ThreadSender) *Service`: after a newly inserted report commits, push each operator `title "Report"`, `href "/app/ops/moderation"`, category `""`, thread `"moderation"`. Best effort, logged.
  - `OpenReports(ctx) ([]ReportGroup, error)`, with `ReportGroup{Kind string; SubjectID uuid.UUID; AuthorID *uuid.UUID; Reasons map[string]int; Count int; Hidden bool; Excerpt string; FirstAt time.Time}`. Subjects with reports and no `dismiss` action after the latest report.
  - `Decide(ctx, operatorID uuid.UUID, kind string, subjectID uuid.UUID, action string) error`, where action is `hide|unhide|dismiss`, calls the Hider and records the action.
- Routes:
  - Web: `POST /app/moderation/terms` (accept, then 303 to the `next` form value, same-origin path only).
  - Web: `GET /app/ops/moderation`, `POST /app/ops/moderation/{kind}/{subjectID}/{action}`, `POST /app/ops/moderation/users/{userID}/suspend` (form `hours`; 0 = open-ended). Non-operators get 404.
  - API: `GET /api/v1/moderation/terms` → `{version, accepted}`; `POST /api/v1/moderation/terms` → 204.
  - Crew report routes come in Tasks 7 and 8.

- [ ] **Step 1: Write failing tests:**
  - `TestCrewMessageAutoHide`:
    - Crew of 4: two reports hide it for everyone; the author's list no longer has it.
    - Crew of 6: two reports don't hide it; the third does.
  - `TestReporterStopsSeeingReported`: right after one report, the reporter's List omits it; others still see it.
  - `TestDecide`: unhide restores it in lists; dismiss drops it from `OpenReports`; a non-operator check lives in the handler test.
  - `TestOperatorPushedOnReport`: fake `ThreadSender` records one call per operator, and none on a duplicate report.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
  - Register the hider in `cmd/web/routes.go` and `cmd/worker/main.go` (the worker needs it only if it reports; register in web only and note why).
  - Ops page: templUI `table` + `button`, one row per group (kind, excerpt, reasons, count, hidden badge, actions).
- [ ] **Step 4: Run, expect PASS**, then `go test ./internal/moderation ./internal/crews ./internal/config`.
- [ ] **Step 5: Commit** `Moderation: crew message hider, reports from crews, terms endpoints, operator queue`.

### Task 5: Live hub + listener

**Files:**
- Create: `internal/crews/live/{hub.go,listener.go}`
- Modify: `cmd/web/server.go`, `cmd/web/routes.go` (background runner runs the Telegram poller and the listener; hub closes on shutdown)
- Test: `internal/crews/live/hub_test.go`, `internal/crews/live/listener_test.go`

**Interfaces — Produces:**
- `func NewHub() *Hub`
- `func (h *Hub) Subscribe(crewID uuid.UUID) (events <-chan Event, cancel func())`, buffer 32.
- `func (h *Hub) Publish(ev Event)`: non-blocking per subscriber. A full buffer drops that event and queues one `resync` for that subscriber instead.
- `func (h *Hub) Resync()`: every subscriber gets `resync`.
- `func (h *Hub) Close()`: closes every subscriber channel. Later Subscribes get a closed channel.
- `func Listen(ctx context.Context, databaseURL string, hub *Hub, log *slog.Logger) error`:
  - Dedicated `pgx.Connect`, `LISTEN crew_events`, `WaitForNotification` loop.
  - Decode failures are logged and skipped.
  - On connection loss, reconnect with backoff 1s doubling to 30s. After a successful reconnect, `hub.Resync()`. Log `"crew live listener connected"` at Info each time.
  - Returns nil when ctx ends.
- Wiring:
  - `routes()` builds the hub and passes it to crews handler/API.
  - Background runs `Listen` alongside the Telegram poller (two goroutines; either error is logged, not fatal).
  - In `server.go`, cancel of the signal ctx calls `hub.Close()` before `srv.Shutdown`. Also `srv.RegisterOnShutdown(hub.Close)`.
  - Add a comment that a transaction pooler (PgBouncer) would break LISTEN.

- [ ] **Step 1: Write failing tests:**
  - `TestHubFanOutPerCrew`: two subs on crew A and one on crew B; publish to A; A's subs get it and B's doesn't.
  - `TestHubSlowSubscriberGetsResync`: fill 32 without reading, publish one more, drain: the last item is `resync`, and no Publish blocked.
  - `TestHubCloseEndsSubscribers`
  - `TestListenDeliversAndResyncsAfterReconnect`:
    - Start `Listen` against testdb; `pg_notify` from the pool; the subscriber gets the event within 2s.
    - Then `pg_terminate_backend` the listener's pid (find it via `pg_stat_activity WHERE query LIKE 'LISTEN%'`); the subscriber gets `resync` within 5s; a later notify still arrives.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, expect PASS** (`-race`), then `go build ./cmd/...`.
- [ ] **Step 5: Commit** `Crews live: one LISTEN connection fans crew events out to subscribers, resync after reconnect`.

### Task 6: SSE streams (API + web)

**Files:**
- Create: `internal/crews/stream.go`
- Modify: `internal/crews/{api.go,handler.go}`
- Test: `internal/crews/stream_test.go`

**Interfaces — Produces:**
- `func serveLive(w http.ResponseWriter, r *http.Request, hub *live.Hub, gate func(live.Event) (frame, bool), heartbeat time.Duration)`:
  - Headers as in `internal/coach/handler.go:401-412`; `http.NewResponseController` flush.
  - Loop: `select` over ctx.Done, events, and a ticker that writes `: ping\n\n`.
  - Ends when the hub channel closes.
- Membership is checked at connect: non-member → 404.
- Per-event gate:
  - Drops `message`/`update` events whose author is blocked either way. Look up the author per event via `MessageByID`; it's cheap.
  - `removed` for the viewer sends the frame, then closes.
  - `removed` for another member is passed on as `channels` (the member list changed).
- `API GET /api/v1/crews/{crewID}/live`:
  - frames `event: <type>\ndata: <Event JSON>\n\n`
  - heartbeat 20s
- `Web GET /app/crews/{crewID}/live?channel={id}` maps events to DOM-event names with empty data:
  - `message` in the open channel → `crew-new`
  - `message` in another channel → `crew-unread`
  - `update` → `msg-<messageID>`
  - `channels` → `crew-channels`
  - `resync` → `crew-resync`
  - `removed` (self) → `crew-removed`

- [ ] **Step 1: Write failing tests** with `httptest.NewServer`, a real hub, and testdb:
  - `TestLiveRejectsNonMember` → 404.
  - `TestLiveAPIStreamsEvents`: connect as a member, publish a message event, read the frame `event: message` with the right seq.
  - `TestLiveDropsBlockedAuthor`: Ana blocks Leo; Leo's message event never reaches Ana's stream; Owner's arrives.
  - `TestLiveRemovedClosesStream`: Remove Ana and publish; Ana's stream gets `removed`, then EOF within 1s.
  - `TestLiveHeartbeat`: heartbeat 50ms in the test; read `: ping`.
  - `TestLiveWebEventNames`: an open-channel message gives `event: crew-new`; another channel gives `crew-unread`; an update gives `msg-<id>`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, expect PASS** (`-race`).
- [ ] **Step 5: Commit** `Crews live: SSE streams for web and API with membership, blocks and heartbeat`.

### Task 7: Push job

**Files:**
- Create: `internal/crews/push.go`
- Modify: `cmd/worker/main.go`
- Test: `internal/crews/push_test.go`

**Interfaces — Consumes:** `SendThread` on the APNs and web-push services (Task 4).

**Interfaces — Produces:**
- `type ThreadSender interface { SendThread(ctx context.Context, userID uuid.UUID, title, body, href, category, threadID string) (int, error) }` lives in `internal/crews`. Moderation uses its own identical interface.
- `func (c *Chat) HandlePushJob(ctx context.Context, payload json.RawMessage) error`.
  - Loads the message; returns nil if gone, deleted or hidden.
  - Recipients: members ≠ author, not blocked either way with the author, `muted_until IS NULL OR muted_until <= now`, filtered by `notify`:
    - `all`: always
    - `mentions`: when mentioned, `mention_all`, or the message replies to one of their messages
    - `none`: never
  - Sends to every `ThreadSender` passed to `WithPushers(...)`:
    - title `"<crew name> · #<channel>"`
    - body `"<author name>: <body>"`
    - href `/app/crews/<crewID>/c/<channelID>`
    - category `"CREW_MESSAGE"`
    - thread `"crew-<crewID>"`
  - Quiet hours are not consulted.
- Worker: `worker.Register(jobs.KindCrewMessagePush, chat.HandlePushJob)`, with the APNs and web-push services as pushers.

- [ ] **Step 1: Write failing tests:**
  - `TestPushRecipients` with a recording fake. Members: owner (`notify=all`), Ana (default mentions), Leo (`none`), Mia (`all`, muted until +1h), Zed (`all`, blocked by the author).
    - A plain message reaches only the owner.
    - `@ana` reaches the owner and Ana.
    - A reply to Leo's message: Leo is still not reached (`none`).
    - Mia and Zed are never reached.
    - The title, href, category and thread are exactly as specified.
  - `TestPushSkipsDeletedOrHidden`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, expect PASS**, then `go test ./internal/apns ./internal/push ./internal/crews` and `go build ./cmd/...`.
- [ ] **Step 5: Commit** `Crews chat: push new messages to who asked for them, threaded per crew`.

### Task 8: JSON API + OpenAPI

**Files:**
- Modify: `internal/crews/api.go`, `docs/api/openapi.yaml`, `cmd/web/api.go` (mount moderation API)
- Test: `internal/crews/api_contract_test.go`, new goldens `testdata/crew-space.golden.json`, `testdata/crew-messages.golden.json`, `cmd/web/api_spec_test.go` (existing, must pass)

**Interfaces — Produces (under `/api/v1`, all bearer-auth):**
- `GET /crews/{crewID}/space` → `CrewSpace{crew CrewView, myRole, notify, mutedUntil?, channels [CrewChannel{id,name,topic,kind,unread,mentions}], members [CrewPerson{id,name,handle,role}]}`
- `POST /crews/{crewID}/channels` `{name,topic}` → 201 `CrewChannel`
- `PATCH /crews/{crewID}/channels/{channelID}` → `CrewChannel`
- `DELETE /crews/{crewID}/channels/{channelID}` → 204
- `GET /crews/{crewID}/channels/{channelID}/messages?before=&after=&limit=` → `CrewMessagePage{messages [CrewMessage], hasMore}`
- `POST /crews/{crewID}/channels/{channelID}/messages` `{body, replyToId?, clientId?}` → 201 `CrewMessage`
- `GET /crews/{crewID}/channels/{channelID}/pins` → `{messages}`
- `POST /crews/{crewID}/channels/{channelID}/read` `{seq}` → 204
- `GET`, `PATCH` `{body}`, `DELETE` `/crews/{crewID}/messages/{messageID}`
- `PUT`, `DELETE` `/crews/{crewID}/messages/{messageID}/reactions/{emoji}` → 204
- `PUT`, `DELETE` `/crews/{crewID}/messages/{messageID}/pin` → 204
- `POST /crews/{crewID}/messages/{messageID}/report` `{reason, note?}` → 204
- `PUT /crews/{crewID}/notifications` `{notify, mutedUntil?}` → 204
- `GET /crews/unread` → `{crews [{crewId, unread, mentions}], total}`
- `GET /crews/{crewID}/live` (text/event-stream)
- `CrewMessage` shape:
  - `{id, seq, channelId, kind, body, author CrewPerson|null, replyTo {id, authorName, excerpt, unavailable}|null, mentionsMe, reactions [{emoji, count, mine}], pinned, edited, deleted, createdAt}`
  - System messages have `body` translated to the request language (`Accept-Language` via the existing i18n middleware).
- `CrewView` gains optional `unread`, `mentions`.
- Error mapping:
  - `moderation.ErrTermsRequired` → 403 `{"error":"terms_required"}`
  - `ErrSuspended` → 403 `{"error":"suspended"}`
  - The rest via `httpx.Error`.
- Bodies: `readJSON` max 16 KiB for messages.

- [ ] **Step 1: Write failing tests:**
  - Contract tests rendering `CrewSpace` and `CrewMessagePage` from fixed fixtures against goldens. Include a system message, a deleted one, a reply with `unavailable`, and reactions.
  - `TestSendTermsRequired403`.
  - `api_spec_test` fails until the spec lists the new routes.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement handlers and the spec.** Describe enum-like fields with known values; mark all new response fields optional except `id`.
- [ ] **Step 4: Run, expect PASS** (`go test ./internal/crews ./cmd/web`). Regenerate goldens only with the repo's update flag, then read the diff.
- [ ] **Step 5: Commit** `Crews API: space, channels, messages, reactions, pins, reads, reports, unread and live`.

### Task 9: Web crew space (3-pane)

**Files:**
- Modify: `web/shared/layout/app.templ` (extract `appShell(title, user, nav, wide bool)`; `App` = not wide; new `AppWide` = sidebar `Collapsed: true`, `<main class="min-h-0 flex-1 overflow-hidden">`)
- Create: `web/crews/{space.templ,messages.templ,channels.templ,composer.templ}`, `web/assets/js/shared/crew-space.js` (only if Alpine inline can't do scroll pinning and read marking; prefer the `x-data` pattern from `web/chat/chat.templ:356`)
- Modify: `web/crews/crews.templ` (the board becomes `ChallengePane` inside the space), `internal/crews/handler.go`, `web/shared/layout/destinations.go` (no move; Crews stays in Progress)
- Modify: `internal/shared/i18n/{en,es,pt_pt,pt_br}_crews.go`
- Test: `web/crews/space_test.go`, `internal/crews/handler_test.go`

**Routes (web, under `/app`, CSRF on POST):**
- Pages:
  - `GET /crews/{crewID}` → the space with the default channel.
  - `GET /crews/{crewID}/c/{channelID}` → the space with that channel.
  - `GET /crews/{crewID}/challenge` → the space with the Challenge tab.
- Fragments:
  - `GET /crews/{crewID}/c/{channelID}/messages?after=|before=`
  - `GET /crews/{crewID}/m/{messageID}`
  - `GET /crews/{crewID}/m/{messageID}/edit`
  - `GET /crews/{crewID}/channels` (channel list)
  - `GET /crews/{crewID}/c/{channelID}/pins`
- Writes (each returns 204 unless noted):
  - `POST /crews/{crewID}/c/{channelID}/messages`
  - `POST /crews/{crewID}/m/{messageID}/{edit|delete|react|pin|report}`. `edit` returns the message fragment; `react` and `pin` take a form `on`.
  - `POST /crews/{crewID}/c/{channelID}/read`
  - `POST /crews/{crewID}/channels`, `.../channels/{channelID}/edit`, `.../channels/{channelID}/delete`
  - `POST /crews/{crewID}/notifications`
- Existing POSTs (role/remove/challenge) now 303 to `/app/crews/{crewID}/challenge`.

**Layout (desktop `lg+`), left to right:**
- **Crew rail** (`w-16`): an avatar fallback with initials per crew from `ListMine`, an unread dot (a `badge`), and the current crew ringed.
- **Channel list** (`w-60`): crew name + a `dropdown` (Notifications…, Invite link copy via `copybutton`, Leave). Then `# name` rows with unread count `badge` and mention count; managers get a `+` that opens a `dialog` (name, topic).
- **Chat pane:**
  - Header: `# name`, topic, a `tabs` strip (Chat | Challenge), a pins button that opens a right `sheet`, and a member count.
  - Transcript: auto-scroll pinned to the bottom only when already near it.
  - Composer: a `textarea` with AutoResize. Enter sends and Shift+Enter makes a newline. A reply preview appears when replying.
- **Below `lg`:** rail hidden; the channel list moves into a left `sheet` opened from the header.

**Live wiring (htmx 4):**
- `<div id="crew-live" hx-sse:connect="/app/crews/{id}/live?channel={ch}">` wraps the pane.
- `#crew-cursor`: a hidden input, `name="after"`, value = last seq.
- `#crew-tail` loader:
  - `hx-get=".../messages"`, `hx-include="#crew-cursor"`
  - `hx-trigger="crew-new from:#crew-live, crew-sent from:body"`, `hx-sync="this:queue last"`
  - `hx-target="#crew-messages"`, `hx-swap="beforeend"`
  - The fragment ends with `<input id="crew-cursor" hx-swap-oob="true" …>`.
- Composer: `hx-post`, `hx-swap="none"`, and `hx-on::after:request` dispatches `crew-sent` on `document.body` and resets on status <400.
  - 403 `terms_required` opens the terms `dialog`, whose form posts to `/app/moderation/terms` with `next` set to the current URL.
  - Field errors render under the textarea via an `hx-target` on an error slot.
- Each message `<article id="m-{id}">` carries `hx-get="/app/crews/{c}/m/{id}"`, `hx-trigger="msg-{id} from:#crew-live"`, `hx-swap="outerHTML"`.
- Channel list: `hx-get=".../channels"`, `hx-trigger="crew-unread from:#crew-live, crew-channels from:#crew-live"`, `hx-swap="outerHTML"`.
- `crew-resync` re-gets the pane with `hx-select="#crew-pane"`.
- `crew-removed` gets `/app/crews` via `HX-Redirect` (`internal/capture/handler.go:173` pattern).
- `.../read` is posted on load and after each `crew-new` while `document.visibilityState=="visible"`.

**Message row:**
- Consecutive messages from one author within 5 minutes collapse the avatar and name.
- System rows are centred, muted, and translated from their key.
- A reply quote is shown above the body.
- `@handle` tokens are highlighted, and the viewer's own mention row is tinted. This is server-side rendering of escaped text; never `templ.Raw` on user text.
- Reactions are pill `button`s that toggle via `hx-post` `.../react` with `hx-swap="none"`.
- An actions `dropdown` (Reply · React ▸ 8 emoji · Edit (own) · Delete (own/manager) · Pin (manager) · Report (others) · Block (others → existing `POST /app/friends/{userID}/block`)).
- The report `dialog` has a reason radio list (`spam harassment hate sexual violence self_harm other`) and an optional note.
- "Load older": a `button` at the top, `hx-get ...?before={firstSeq}`, `hx-swap="outerHTML"`; the fragment returns older rows plus a new button when `HasMore`.
- Empty channel: `emptystate`.

**Copy (en; translate to es/pt_pt/pt_br):**
- `crews.sys.spaces` = "This crew now has channels. Everyone in the crew can read what's posted here."
- `crews.sys.welcome` = "Welcome to #general. Say hi — everyone in the crew can read this channel."
- `crews.intro` = "A few people keeping each other going. Crewmates see who checked in and trained today, streaks and the week's challenge, and can chat in the crew's channels."
- `crews.join.desc.one` / `.many` use the same rewrite. Drop "Nothing else."; add "Messages you post are seen by everyone in the crew."
- `crews.err.full` = "This crew is full."
- `crews.err.blocked-join` = "You can't join this crew."
  - Move both P0 strings in `service.go` to `i18n.T`.
- `crews.err.body` = "Write something under 4000 characters."
- `crews.err.blocked` = "That message breaks the crew rules."
- `crews.err.slow` = "Slow down a little and try again in a minute."
- `moderation.terms.*`: title "Crew rules"; body "No hate, harassment, sexual content involving anyone without consent, threats or spam. Content that breaks these rules is removed and accounts that post it are suspended. Report anything that crosses the line."; button "I agree".
- Plus labels for every control above.

- [ ] **Step 1: Write failing tests:**
  - `TestSpaceRendersChannelsAndMessages`: render `SpacePage` with fixtures and assert:
    - each channel name appears
    - `hx-sse:connect` points at the live URL with the channel
    - `#crew-cursor` value equals the last seq
    - a system message is rendered translated, not as its key
    - a `<script>` in a body is escaped
  - `TestSpaceRendersInTheChosenLanguage` (es) and a no-English-leak check, modelled on `web/crews/i18n_test.go`.
  - `TestMessagesFragmentAfterCursor` (handler test, testdb): `GET .../messages?after=N` returns only newer rows and the OOB cursor.
  - `TestComposerTermsRequired`: POST without terms → 403 with a marker the composer recognises.
  - `TestOldCrewURLStillWorks`: `GET /app/crews/{id}` → 200 with the space.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** templates, handlers and copy. `templ generate`.
- [ ] **Step 4: Run, expect PASS**, then `go test ./web/... ./internal/crews ./internal/shared/i18n`.
- [ ] **Step 5: Commit** `Crews web: Discord-style crew space with channels, live chat, reactions, replies, pins and reports`.

### Task 10: Export + verification

**Files:**
- Modify: `internal/export/export.go` (+ `cmd/web/routes.go` wiring)
- Test: `internal/export/export_test.go`

**Interfaces — Produces:**
- `export.CrewMessages interface { AuthoredBy(ctx, userID uuid.UUID) ([]crew.ExportedMessage, error) }`
  - `crew.ExportedMessage{Crew, Channel, Body string; CreatedAt time.Time; Edited, Deleted bool}`
  - Written to `crews/messages.json`. Deleted messages are excluded.

- [ ] **Step 1: Write the failing test** `TestExportIncludesCrewMessages`: an authored message appears; one from another member doesn't.
- [ ] **Step 2: FAIL → implement → PASS.** Commit `Export: messages you posted in crews`.
- [ ] **Step 2b: Update the Daily Post plan.**
  - In `~/.claude/plans/tranquil-hatching-mango.md` (outside the repo), replace its planned `comment_reports` table with `internal/moderation` `content_reports` plus a registered `Hider`.
  - Update the [[daily-post-plan]] memory to match.
- [ ] **Step 3: Full verification:**
  - `sqlc generate` and `templ generate` with no diff
  - `go vet ./...`
  - gofumpt clean
  - `go test -p 4 ./...` with output to a file; read the tail
  - `scripts/sync-openapi.sh --check` if present
- [ ] **Step 4: Manual check** (required this time; htmx 4 SSE behaviour is not unit-testable):
  - Setup:
    - `go run ./cmd/web` with `AI_PROVIDER=fake`.
    - Two browsers: Chrome via claude-in-chrome, or two headless sessions per the `north-headless-screenshots` memory recipe.
    - Two users in one crew.
  - Messaging:
    - A sends; B sees it in under 1s without reload.
    - B reacts; A's pill updates.
    - A edits; B sees "edited".
    - A deletes; B sees the placeholder.
    - Load older works after 60 messages.
    - Terms dialog on the first post.
  - Live connection:
    - Kill the listener with `pg_terminate_backend`; both resync; the next message arrives.
    - Remove B; B's page goes to `/app/crews`.
    - Ctrl-C the server with a stream open: it exits in under 2s.
  - Screens: screenshot desktop and 390px widths, light and dark.
  - Report what was and wasn't checked.
- [ ] **Step 5: Commit** any fixes, then push and open the PR only on the user's go-ahead.

## Out of scope (later phases or polish)

Mention autocomplete, typing indicators, link previews, message search, and crew admins viewing their crew's reports (operators handle reports in P1). Media, boards and threads are P3; wins and the Challenge redesign are P2; iOS is P1i. `apns-collapse-id` is unneeded: chat uses thread-id, and per-message collapse would hide messages.

## Deploy notes

- `OPERATOR_USER_IDS` goes in `platform/infra/apps/khepri/web/values-production.yaml` env. It is not a secret; the worker doesn't need it.
- No network-policy change.
- SSE through Traefik/Cloudflare relies on the 20s heartbeat. Check prod logs for `crew live listener connected` after rollout.
- Manual tag bump PR in infra (the promote PR is broken).
