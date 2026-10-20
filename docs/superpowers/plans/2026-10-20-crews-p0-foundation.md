# Crews P0 — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lay the server-side ground for crews-as-groups without anything user-visible except roles and bigger caps.
The ground is channels (`#general`, `#wins`), roles, visibility, block-safe joins, and a shared moderation slice.

**Architecture:** One migration extends `crews`/`crew_members` and adds `crew_channels` with an idempotent backfill. `internal/crews` gains roles and channels.
A new vertical slice `internal/moderation` (own sqlc package) holds reports, actions, UGC terms and suspensions, plus a pure `Filter`. P1 (chat) and Daily Post phase 2 are its first consumers.

**Tech Stack:** Go, pgx/v5, sqlc (per-slice), goose (timestamp migrations), templ + templUI, `golang.org/x/sync/errgroup`, `golang.org/x/text/unicode/norm` (both already in go.mod).

**Spec:** `~/.claude/plans/groups-discord-like-servers-with-parallel-brook.md` (approved 2026-10-20). P0 row of its phase table.

## Global Constraints

- Work only in worktree `.worktrees/crews-p0`, branch `feat/crews-p0`. The shared checkout switches branches under other sessions.
- Use a private Go cache for every go command: `export GOCACHE=$PWD/.gocache` (add `.gocache/` to `.git/info/exclude`, not `.gitignore`).
- DB tests need `TEST_DATABASE_URL` (local Postgres on 5434). If OrbStack stopped, run `orb start && docker start north-client-postgres-1`.
- Migration filenames: UTC digits only, e.g. `20261020120000_crew_spaces.sql`. `TestEveryMigrationFilenameIsParseable` enforces it.
- Backfills must be idempotent and fast: prod runs them once in the PreSync hook with `AUTO_MIGRATE=false`.
- Ownership stays in `crews.owner_id`. `crew_members.role` holds only `admin|member`, and the API reports `owner|admin|member`.
- Caps: `MaxMembers = 25`, `MaxMembersPro = 100`, with the comment `// Whether larger crews are a paid feature is decided in a later phase.`
- Shipped iOS decodes some enums strictly. New API fields are additive and optional. `role` is `type: string` with known values in the description, never an `enum:`.
- Any new user-facing string goes into all four catalogues: `en_`, `es_`, `pt_pt_`, `pt_br_` (`TestEveryCatalogueCoversEnglish`).
- Do **not** change the "Nothing else" privacy copy or post any system message in P0. Nothing new is visible to crewmates yet; that copy changes in P1 when chat ships.
- New table names must not collide: `reports`, `messages` and `invites.channel` already exist.

## Review Focus

1. **An owner who has blocked someone, and that person joins by code.** Refused with "You can't join this crew." The wording must not reveal the block. Same in the reverse direction. *Task 3.*
2. **Existing crews after the migration.** Every pre-existing crew has exactly one `#general` (default) and one `#wins`. Running Down then Up again doesn't duplicate them or fail. *Task 1.*
3. **Owner leaves a crew that has an admin.** Ownership passes to the longest-standing **admin**, not just the oldest member. That person's admin row stays consistent: the new owner's role doesn't matter because ownership is `owner_id`. *Task 2.*
4. **An admin tries to remove the owner or another admin, or to change roles.** Refused with `ErrNotFound`, matching existing owner-only refusals. *Task 2.*
5. **Filter false positives on gym talk** ("this set was f***ing brutal", "Scunthorpe", "shitake", "assassin"). Allowed; only listed slurs and hate terms match, whole-word, after normalisation. *Task 5.*

---

### Task 1: Crew spaces migration + backfill

**Files:**
- Create: `migrations/20261020120000_crew_spaces.sql`
- Test: `internal/shared/database/crew_spaces_migration_test.go`

**Interfaces:**
- Produces (schema):
  - `crews.visibility text NOT NULL DEFAULT 'invite' CHECK (visibility IN ('invite','friends','open'))`
  - `crews.description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 200)`
  - `crew_members.role text NOT NULL DEFAULT 'member' CHECK (role IN ('admin','member'))`
  - `crew_members.notify text NOT NULL DEFAULT 'mentions' CHECK (notify IN ('all','mentions','none'))`
  - `crew_members.muted_until timestamptz`
  - `crew_channels(id uuid PK default gen_random_uuid(), crew_id uuid NOT NULL → crews ON DELETE CASCADE, kind text NOT NULL CHECK (kind IN ('text','board','wins')), name text NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9-]{0,29}$'), topic text NOT NULL DEFAULT '' CHECK (char_length(topic) <= 120), position smallint NOT NULL DEFAULT 0, is_default boolean NOT NULL DEFAULT false, created_by uuid → users ON DELETE SET NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE (crew_id, name))`
  - `CREATE UNIQUE INDEX crew_channels_one_wins ON crew_channels (crew_id) WHERE kind = 'wins'`
  - `CREATE UNIQUE INDEX crew_channels_one_default ON crew_channels (crew_id) WHERE is_default`

- [ ] **Step 1: Write the failing test** `TestCrewSpacesMigrationBackfillsChannels`. Copy the harness from `meal_options_migration_test.go`, with its own DB name `north_crew_spaces_check`.
  - `UpTo(20261016140000)`, the last migration on main before this one.
  - Insert one user and one crew (`code 'abcdefghjk'`) with the owner in `crew_members`.
  - `UpTo(20261020120000)`.
  - Assert: `SELECT kind, name, is_default, position FROM crew_channels WHERE crew_id=$1 ORDER BY position` returns `[("text","general",true,0), ("wins","wins",false,1)]`.
  - Assert the crew's `visibility = 'invite'`, and the owner's `crew_members.role = 'member'`. The owner is not an admin row; ownership is `owner_id`.
  - `DownTo(20261016140000)` then `UpTo(20261020120000)` again. Assert there are still exactly 2 channels.
- [ ] **Step 2: Run, expect FAIL.** `go test ./internal/shared/database -run TestCrewSpacesMigration -v` fails at "migrate to 20261020120000" (no such version).
- [ ] **Step 3: Write the migration.**
  - Up: the DDL above, then the backfill:
    - `INSERT INTO crew_channels (crew_id, kind, name, is_default, position) SELECT id, 'text', 'general', true, 0 FROM crews ON CONFLICT DO NOTHING;`
    - the same for `('wins','wins',false,1)`.
  - Comment above `crew_channels`: crews are spaces now. `#general` is undeletable, and `#wins` is where shared achievements land (P2).
  - Down: drop the table, then the five columns.
- [ ] **Step 4: Run, expect PASS** (same command). Also `go test ./internal/shared/database -run TestEveryMigrationFilenameIsParseable`.
- [ ] **Step 5: Commit.** `git commit -m "Crews: channels, roles and visibility columns, backfill #general and #wins"`

### Task 2: Roles and channels in the crews service

**Files:**
- Modify: `internal/crews/crew/crew.go`, `internal/crews/db/queries.sql` (then `sqlc generate`), `internal/crews/service.go`
- Test: `internal/crews/service_test.go`

**Interfaces:**
- Produces in `crew`:
  - `type Role string`
  - `const (RoleOwner Role = "owner"; RoleAdmin Role = "admin"; RoleMember Role = "member")`
  - `Member.Role Role`
  - `Board.MyRole Role`. Keep `Board.IsOwner` and `Member.Owner` for existing callers.
  - `const (ChannelText = "text"; ChannelBoard = "board"; ChannelWins = "wins")`
  - `type Channel struct { ID uuid.UUID; Kind, Name, Topic string; Position int; IsDefault bool }`
  - `func (r Role) CanManage() bool`: true for owner and admin.
- Produces in `crews.Service`:
  - `func (s *Service) SetRole(ctx context.Context, crewID, ownerID, memberID uuid.UUID, role crew.Role) error`
  - `func (s *Service) Channels(ctx context.Context, crewID, viewerID uuid.UUID) ([]crew.Channel, error)`
- New queries:
  - `CreateDefaultChannels :exec`: one INSERT of both rows, the same values as the backfill.
  - `ListChannels :many`: ordered by position, name.
  - `MemberRole :one`: `SELECT role FROM crew_members WHERE crew_id=$1 AND user_id=$2`.
  - `SetMemberRole :execrows`.
  - Change `OldestOtherMember` to `ORDER BY (role = 'admin') DESC, joined_at`.
  - `Members` adds `m.role`.

- [ ] **Step 1: Write failing tests:**
  - `TestCreateMakesGeneralAndWins`: `svc.Create`, then `svc.Channels` returns names `["general","wins"]`, kinds `["text","wins"]`, and `[0].IsDefault`.
  - `TestRoles`: create Ana's crew; Leo, Mia and Zed join.
    - `SetRole(c, ana, leo, RoleAdmin)` → nil, and Board as Leo has `MyRole == RoleAdmin`.
    - `SetRole(c, leo, mia, RoleAdmin)` → `ErrNotFound` (only the owner sets roles).
    - `SetRole(c, ana, ana, RoleMember)` → FieldErrors key `"role"`.
    - `SetRole(c, ana, mia, RoleOwner)` → FieldErrors key `"role"`.
    - Admin Leo: `SetChallenge` succeeds and `Remove(c, leo, mia)` succeeds.
    - `Remove(c, leo, ana)` → `ErrNotFound`.
    - Promote Zed to admin; `Remove(c, leo, zed)` → `ErrNotFound`.
  - `TestOwnerLeavingPrefersAdmin`: Ana's crew; Leo joins, then Mia joins and is made admin. Ana leaves → Board owner is Mia, not Leo.
  - `TestChannelsMembersOnly`: a non-member gets `ErrNotFound`.
- [ ] **Step 2: Run, expect FAIL.** `go test ./internal/crews -run 'TestCreateMakesGeneralAndWins|TestRoles|TestOwnerLeavingPrefersAdmin|TestChannelsMembersOnly' -v` fails to compile (`SetRole`, `Channels` and `RoleAdmin` don't exist).
- [ ] **Step 3: Implement.**
  - `Create` calls `CreateDefaultChannels` inside its existing tx.
  - Introduce a private `role(ctx, c crewsdb.Crew, userID) (crew.Role, error)`: owner if `c.OwnerID == userID`, else the `MemberRole` value.
  - `SetChallenge` and `Remove` allow `CanManage()`.
  - In `Remove`, a non-owner manager may only remove a target whose role is `member`; otherwise return `ErrNotFound`.
  - `Board` fills `Member.Role` and `MyRole`.
- [ ] **Step 4: Run, expect PASS** (same command), plus the full `go test ./internal/crews`.
- [ ] **Step 5: Commit.** `git commit -m "Crews: owner/admin/member roles and default channels"`

### Task 3: Block-safe join and bigger caps

**Files:**
- Modify: `internal/crews/service.go`, `internal/crews/crew/crew.go`, `cmd/web/routes.go:~311`, `cmd/worker/main.go:~377,~469`, `internal/shared/i18n/{en,es,pt_pt,pt_br}_crews.go`
- Test: `internal/crews/service_test.go`

**Interfaces:**
- Produces:
  - `type BlockChecker interface { Blocked(ctx context.Context, a, b uuid.UUID) (bool, error) }` in `internal/crews`.
  - `func (s *Service) WithBlocks(b BlockChecker) *Service`. `*social.Repository` already satisfies it (`internal/social/repository.go:259`).
- Consumes: Task 2's service.

- [ ] **Step 1: Write failing tests:**
  - `TestJoinRefusedWhenBlocked`: Ana creates a crew; `social.NewRepository(pool).Block(ctx, ana, leo)`; `svc.WithBlocks(socialRepo).Join(leo, code)` returns FieldErrors key `"crew"` with message `"You can't join this crew."`, and Leo is not a member. Repeat with Leo blocking Ana: same result.
  - `TestJoinCaps`: a free owner's crew rejects member 26; a Pro owner's crew accepts member 26. Insert members with `AddMember` directly for speed; set tier with `UPDATE users SET tier='pro'`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
  - In `Join`, after the membership short-circuit and before the cap, if `s.blocks != nil` and `Blocked(userID, c.OwnerID)`, return FieldErrors `crew` / `"You can't join this crew."`.
  - Set the caps and comment per Global Constraints.
  - Wire `.WithBlocks(socialRepo)` at all three `crews.NewService` call sites. Find the existing social repository variable in each file; create one with `social.NewRepository(pool)` if absent.
  - Copy: change `crews.start.desc` from "Up to eight people." to "Up to 25 people." in all four locales (es "Hasta 25 personas.", pt-PT/pt-BR "Até 25 pessoas."), keeping the second sentence.
  - Add the error string as `crews.join.blocked` if the handler renders FieldErrors through i18n; check `internal/crews/handler.go` and follow whatever the "This crew is full." path does.
- [ ] **Step 4: Run, expect PASS.** `go test ./internal/crews ./internal/shared/i18n ./cmd/...`
- [ ] **Step 5: Commit.** `git commit -m "Crews: refuse joins across a block; caps 25 free / 100 Pro"`

### Task 4: Board fills members concurrently

At 100 members the board makes about 400 sequential queries (`fill`, `internal/crews/service.go`). Bound it rather than redesign the check-in and activity readers.

**Files:**
- Modify: `internal/crews/service.go` (`Board`)
- Test: `internal/crews/service_test.go`

- [ ] **Step 1: Write a failing test** `TestBoardKeepsJoinOrder`: 12 members join in sequence. `Board` lists them in join order with the correct `Me` and `Role`. It guards ordering once fills run in parallel.
- [ ] **Step 2: Run.** It passes today and must still pass after Step 3. Record the baseline.
- [ ] **Step 3: Implement.**
  - Preallocate `out.Members = make([]Member, len(rows))`.
  - Fill each index inside `errgroup.WithContext` with `g.SetLimit(8)`.
  - Set `WeekStart` after `g.Wait()` from the viewer's row.
- [ ] **Step 4: Run** `go test ./internal/crews -race`. Expect PASS.
- [ ] **Step 5: Commit.** `git commit -m "Crews: fill the board eight members at a time"`

### Task 5: Moderation filter (pure)

**Files:**
- Create: `internal/moderation/filter.go`, `internal/moderation/wordlists/{en,es,pt}.txt` (embedded via `//go:embed wordlists/*.txt`)
- Test: `internal/moderation/filter_test.go`

**Interfaces:**
- Produces:
  - `func NewFilter() *Filter`
  - `func (f *Filter) Check(text string) error`: returns `ErrBlockedContent` on a match.
  - `var ErrBlockedContent = errors.New("blocked content")`
  - All locales' lists are checked together, since crews mix languages.

- [ ] **Step 1: Write the failing table test** `TestFilter`:
  - Allowed (nil): `"this set was fucking brutal"`, `"Scunthorpe"`, `"shitake risotto"`, `"assassin"`, `"cabrón"` (Spanish banter), `""`.
  - Rejected: each list's first entry, its upper-cased form, a leetspeak form (`a→4/@`, `e→3`, `i→1/!`, `o→0`, `s→5/$`), and a diacritics form.
  - Use the list's own entries via an exported test helper `moderation.TermsForTest()` in an `export_test.go`, so no slur appears in test source.
- [ ] **Step 2: Run, expect FAIL** (package missing).
- [ ] **Step 3: Implement.**
  - Normalise: NFKD (`x/text/unicode/norm`), drop combining marks, lower-case, map the leetspeak table, then collapse non-letters to spaces.
  - Match whole words; multi-word terms match as consecutive words.
  - Lists: slurs and hate and sexual-violence terms only. Never ordinary swearing. Every file starts with a comment saying so.
- [ ] **Step 4: Run, expect PASS.** `go test ./internal/moderation -run TestFilter -v`
- [ ] **Step 5: Commit.** `git commit -m "Moderation: word filter for slurs and hate, not swearing"`

### Task 6: Moderation service — reports, auto-hide, terms, suspensions

**Files:**
- Create: `migrations/20261020130000_moderation.sql`, `internal/moderation/db/queries.sql` (add a `moderationdb` block to `sqlc.yaml`, copied from the `crewsdb` block), `internal/moderation/moderation.go`, `internal/moderation/service.go`
- Test: `internal/moderation/service_test.go`

**Interfaces:**
- Schema: `content_reports`, `moderation_actions`, `ugc_terms_acceptances`, `ugc_suspensions`, exactly as in the spec's "Shared moderation tables" section.
- Produces:
  - `type Subject struct { Kind string; ID uuid.UUID; AuthorID uuid.UUID; ScopeID *uuid.UUID }`
  - `type Hider interface { Hide(ctx context.Context, subjectID uuid.UUID) error; Threshold(ctx context.Context, subjectID uuid.UUID) (int, error) }`
  - `func NewService(pool *pgxpool.Pool) *Service`
  - `func (s *Service) Register(kind string, h Hider)`
  - `func (s *Service) Report(ctx context.Context, reporterID uuid.UUID, sub Subject, reason, note string) (hidden bool, err error)`
  - `func (s *Service) ReportedBy(ctx context.Context, reporterID uuid.UUID, kind string, ids []uuid.UUID) (map[uuid.UUID]bool, error)`: hides an item from its reporter at once.
  - `const TermsVersion = 1`
  - `func (s *Service) TermsAccepted(ctx context.Context, userID uuid.UUID) (bool, error)`
  - `func (s *Service) AcceptTerms(ctx context.Context, userID uuid.UUID) error`
  - `func (s *Service) Suspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID, until *time.Time, reason string) error`
  - `func (s *Service) Unsuspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID) error`
  - `func (s *Service) Suspended(ctx context.Context, userID uuid.UUID, now time.Time) (bool, error)`
  - `func (s *Service) CanPost(ctx context.Context, userID uuid.UUID) error`: `ErrTermsRequired` if terms aren't accepted; `ErrSuspended` if suspended.
  - Reasons: `spam, harassment, hate, sexual, violence, self_harm, other`.

- [ ] **Step 1: Write failing tests**, with a fake `Hider` recording calls and returning threshold 3:
  - `TestReportAutoHidesAtThreshold`: reports from 3 distinct users. The first two return `hidden=false` and the third returns `hidden=true`. `Hide` was called once, and one `moderation_actions` row has `action='hide'` and `actor_id IS NULL`.
  - `TestReportTwiceIsNoop`: the same reporter twice gives 1 row and no error.
  - `TestReportOwnContentRefused`: `reporter == AuthorID` → FieldErrors `"report"`.
  - `TestReportUnknownKindOrReason`: unregistered kind → `apperr.ErrNotFound`; reason `"meh"` → FieldErrors `"reason"`; note over 500 runes → FieldErrors `"note"`.
  - `TestReportedBy`: returns true only for the reporter's subjects.
  - `TestCanPost`: a new user → `ErrTermsRequired`; after `AcceptTerms` → nil; `Suspend` with `until = now+1h` → `ErrSuspended`; with the clock at now+2h → nil; `Unsuspend` → nil.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
  - `Report` inserts `ON CONFLICT DO NOTHING`, then counts distinct reporters for the subject.
  - If count ≥ `Threshold`, call `Hide` and insert the action, in one tx with the insert.
  - `Hide` must be idempotent; document that on the interface.
  - The service takes a clock (`WithClock`) like `crews.Service`.
- [ ] **Step 4: Run, expect PASS.** `sqlc generate && go test ./internal/moderation -v`
- [ ] **Step 5: Commit.** `git commit -m "Moderation: reports with auto-hide, UGC terms, suspensions"`

### Task 7: Roles on the API and the web board; reserved handles

**Files:**
- Modify: `internal/crews/api.go` (MemberView `Role string \`json:"role"\``, BoardView `MyRole string \`json:"myRole"\``, new route), `docs/api/openapi.yaml`, `internal/crews/testdata/crew-board.golden.json`, `internal/crews/handler.go`, `web/crews/crews.templ`, `internal/shared/i18n/*_crews.go`, `internal/social/service.go:50`
- Test: `internal/crews/api_contract_test.go`, `cmd/web/api_spec_test.go`, `internal/social/service_test.go`

**Interfaces:**
- `PUT /api/v1/crews/{crewID}/members/{userID}/role` takes `{"role":"admin"|"member"}`. It returns 200 with `CrewBoard`, or 404/422. Its operationId is `setCrewMemberRole`.
- Web: `POST /app/crews/{id}/members/{uid}/role` with form `role`, then a redirect to the board.

- [ ] **Step 1: Write failing tests.**
  - Regenerate the golden with `role` and `myRole` and assert they are present. Use the existing golden-update flag in `api_contract_test.go`.
  - `api_spec_test` fails until the route is in the spec.
  - In `TestSetHandle`'s table, add `"everyone"`, `"here"` and `"crew"` as reserved.
- [ ] **Step 2: Run, expect FAIL.** `go test ./internal/crews ./internal/social ./cmd/web -run 'Contract|Spec|Handle'`
- [ ] **Step 3: Implement.**
  - Spec: add `role` and `myRole` as optional `type: string`, with the description "owner, admin or member; treat unknown values as member". Do not add them to `required`.
  - Board template:
    - Show an "admin" badge next to the existing owner badge, using the templUI `badge` component as the owner badge does.
    - For the owner, add a member-row form with a templUI button labelled "Make admin" or "Remove admin".
    - Show the challenge card when `b.MyRole.CanManage()`.
    - Show the remove form when the viewer can manage and the row is a plain member.
  - i18n keys `crews.admin`, `crews.role.make-admin` and `crews.role.remove-admin` in all four locales.
  - Add the three reserved handles.
- [ ] **Step 4: Run, expect PASS.** `templ generate && go test ./internal/crews ./internal/social ./cmd/web ./internal/shared/i18n`
- [ ] **Step 5: Commit.** `git commit -m "Crews: roles in the API and on the board; reserve everyone/here/crew"`

### Task 8: Whole-branch verification and PR

- [ ] **Step 1:** `sqlc generate && templ generate && git diff --exit-code` shows no drift. Then `go vet ./... && gofumpt -l internal cmd` prints nothing.
- [ ] **Step 2:** `go test ./... 2>&1 | tail -40`. Expect all `ok` or `skip`, with no FAIL.
- [ ] **Step 3: Manual check.**
  - `task dev` (or the Taskfile's run target) against the local DB.
  - Create a crew as user A; join as B via `/i/c/{code}`.
  - A makes B an admin; B sets the challenge.
  - A blocks C; C's join is refused with "You can't join this crew."
- [ ] **Step 4:** Push the branch and open a PR to `NorthAIProject/north-client` titled "Crews P0: roles, channels, block-safe joins, moderation slice". In the body:
  - Link the spec.
  - State that iOS still says "up to eight" (changed in P1i).
  - State that nothing new is visible to crewmates yet.
- [ ] **Step 5:** After merge, follow the manual promote: bump the khepri tag in `platform/infra/apps/khepri/{web,worker}/values-production.yaml` by PR (memory: north-promote-prs-are-manual).
