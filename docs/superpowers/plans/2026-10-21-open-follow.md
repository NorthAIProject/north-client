# Open Follow (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A follow takes effect at once with no approval, and new people share training, streaks and goals by default. Existing people keep exactly what they share today and get a one-time inbox notice.

**Architecture:** One migration does four things: it accepts every pending follow, writes explicit `shared=false` rows for existing people so their current "off" survives, adds a SQL function `sharing_on(user, category)` that holds the default-on rule, and inserts a one-time notice nudge. Every query that gates on sharing calls `sharing_on`, and Go's `Sharing()` applies the same default. `Follow` writes `accepted` and notes `new_follower`. The web Friends page loses its requests section and its pending states. A separate north-ios PR does the same on iOS.

**Tech Stack:** Go, sqlc (`task sqlc`), goose migrations, templ (`task generate`), Postgres test DB (`TEST_DATABASE_URL`), SwiftUI.

**Spec:** `docs/superpowers/specs/2026-10-21-follow-awards-design.md`, §1 "Open follow".

## Global Constraints

- Default-on categories for people with no stored row: `training`, `streaks`, `goals`. `xp` stays default-off. (`awards` comes in phase 3; do not add it here.)
- Existing people keep their current switches: a missing row today means off, so the migration must write `false` rows before the default flips.
- Shipped iOS builds must keep decoding every response. Keep `requests` in the `/social` overview JSON as an always-empty array. Keep `POST /followers/{id}/accept` answering 204 for an already-accepted follower. Add no enum values.
- Notification kind for a new follower: `new_follower`. Copy: title `"{name} followed you"`, body `"See who follows you in Friends."`, href `/app/friends`.
- The one-time notice is a `user_nudges` row: kind `open_follow`, dedupe key `v1`, title `"Khepri now has followers"`, body `"Anyone with your handle can follow you now. Choose what followers see in Friends."`, href `/app/friends#sharing`.
- Web copy lives in four locales: `internal/shared/i18n/{en,es,pt_pt,pt_br}_social.go`. Every new or changed key goes into all four.
- Run Go tests with a private cache: `GOCACHE=$(mktemp -d) go test ...` (memory `shared-gocache-wiped-mid-test`).

## Review Focus

1. A person who signed up before this deploy and never touched sharing must still share nothing afterwards. Their followers' feed and boards must not suddenly show them. This is covered by a test in Task 1 that simulates a pre-existing row-less user.
2. A shipped iOS build with a stale requests list taps Accept on someone the migration already accepted. It must get 204, not a 404 error toast (Task 3 test).
3. Re-following after being removed must not error and must end `accepted`. The dedupe key keeps the inbox from piling up (Task 3 test).
4. Blocking still beats open follow. A blocked person who follows by handle gets not-found, and a block still removes the follow (the existing `TestBlockHidesBothWays` must stay green; nothing to add).
5. The leaderboard and the feed agree on who is visible. Both go through `sharing_on`, so a new person shows up on a follower's workouts board and in their feed without touching any switch (Task 2 test).

---

### Task 1: Migration: accept pending follows, pin existing switches, `sharing_on`, notice

**Files:**
- Create: `migrations/20261021130000_open_follow.sql`
- Test: `internal/achievements/sharing_default_test.go`

**Interfaces:**
- Produces: the SQL function `sharing_on(p_user uuid, p_category text) RETURNS boolean` (STABLE). It returns the stored `shared` value if a row exists, else `p_category IN ('training','streaks','goals')`.

- [ ] **Step 1: Write the failing test** `TestSharingOnDefaults` against the test DB. Register person A. Call `SELECT sharing_on($1, c)` for each category and assert training/streaks/goals = true and xp = false. Insert `(A, 'training', false)` into `achievement_sharing` and assert `sharing_on(A,'training')` = false.
- [ ] **Step 2: Run** `GOCACHE=$(mktemp -d) go test ./internal/achievements/ -run TestSharingOnDefaults`. Expected: FAIL, `function sharing_on(uuid, unknown) does not exist`.
- [ ] **Step 3: Write the migration.** Up, in this order:
  1. `UPDATE follows SET status='accepted', accepted_at=COALESCE(accepted_at, now()) WHERE status='pending';`
  2. `INSERT INTO achievement_sharing (user_id, category, shared) SELECT u.id, c, false FROM users u CROSS JOIN unnest(ARRAY['training','streaks','goals']) c ON CONFLICT (user_id, category) DO NOTHING;`
  3. `CREATE FUNCTION sharing_on ... LANGUAGE sql STABLE`.
  4. Insert the notice nudge (Global Constraints) for every user, `ON CONFLICT DO NOTHING`.

  Down: drop the function and `DELETE FROM user_nudges WHERE kind='open_follow'`. Comment that the accepted follows and pinned rows are not reversed. Open with a comment in the house style (see `migrations/20261002090000_sharing_xp.sql`) explaining why the rows are pinned before the default flips.
- [ ] **Step 4: Run the test.** Expected: PASS. Also run `task sqlc`, which must succeed now that the schema includes the function.
- [ ] **Step 5: Commit** `feat(social): open follow migration and sharing_on default`.

### Task 2: All sharing gates use `sharing_on`; Go `Sharing()` applies the same default

**Files:**
- Modify: `internal/achievements/db/queries.sql` (`Feed`, `Visible`: replace the `EXISTS (SELECT 1 FROM achievement_sharing ...)` block with `sharing_on(a.user_id, a.category)`)
- Modify: `internal/xp/db/queries.sql` (`Participants`: `sharing_on(u.id, sqlc.arg('category'))`; `Shares`: `SELECT sharing_on(sqlc.arg('user_id'), sqlc.arg('category'))`)
- Modify: `internal/achievements/achievement/achievement.go`: add `func DefaultShared(category string) bool`
- Modify: `internal/achievements/service.go:192` `Sharing()`: start from `DefaultShared` for each category, then let stored rows override
- Regenerate: `task sqlc`
- Test: `internal/achievements/sharing_default_test.go`, `internal/xp/service_test.go`

**Interfaces:**
- Consumes: `sharing_on` (Task 1).
- Produces: `achievement.DefaultShared(category string) bool`, which is true for `CategoryTraining`, `CategoryStreaks` and `CategoryGoals`.

- [ ] **Step 1: Write the failing tests.**
  - `TestNewPersonSharesByDefault`: B registers and A follows B (accepted). `svc.Sharing(B)` equals `Sharing{Training:true, Streaks:true, Goals:true, XP:false}`. Record a training achievement for B; `Feed(A)` contains it. `SetSharing(B, Sharing{})`; `Feed(A)` no longer contains it.
  - `TestPinnedOffStaysOff`: insert `false` rows for B's three categories directly (this mimics the migration on a pre-existing user). B's achievement is not in A's feed, and `Sharing(B)` is all false.
  - In `internal/xp`: `TestNewFollowerOnWorkoutsBoardByDefault`. A follows B, B finishes a workout, and B appears in A's `workouts` board with no switch touched. Follow the existing xp test fixtures.
- [ ] **Step 2: Run** `GOCACHE=$(mktemp -d) go test ./internal/achievements/ ./internal/xp/`. Expected: the new tests FAIL (B missing from the feed and the board).
- [ ] **Step 3: Implement** the query edits, `DefaultShared` and the `Sharing()` default. Run `task sqlc`.
- [ ] **Step 4: Run** the same command. Expected: PASS, including every existing achievements and xp test.
- [ ] **Step 5: Commit** `feat(achievements): sharing defaults on for new people via sharing_on`.

### Task 3: Follow is instant; `new_follower` note; idempotent Accept

**Files:**
- Modify: `internal/social/service.go:27-31` (replace `KindFollowRequest` with `KindNewFollower = "new_follower"`), `:162-192` (`Follow`), `:195-205` (`Accept`), `:232-262` (`Overview`: stop splitting out pending)
- Modify: `internal/social/db/queries.sql:44-50`: update the comment only. The upsert already promotes to accepted when `status='accepted'` is passed.
- Modify: `internal/social/api.go:84`: comment that `requests` is always empty and kept for shipped iOS
- Test: `internal/social/service_test.go`: replace `TestFollowNeedsApproval` (`:144`) with `TestFollowIsInstant`

**Interfaces:**
- Produces: `social.KindNewFollower`. `Follow` returns `Connection{Status: StatusAccepted}`. `Accept` returns nil when the follow is already accepted, and `apperr.ErrNotFound` only when there is no follow at all.

- [ ] **Step 1: Write the failing test** `TestFollowIsInstant`:
  - Ana follows `@joao` and gets `c.Status == social.StatusAccepted`.
  - A second `Follow` returns no error, and `inbox.notes` has exactly one entry: `{to: joao, kind: social.KindNewFollower, title: "Ana followed you"}`.
  - `Overview(joao)` has `Followers` of length 1 and `Requests` of length 0.
  - `Accept(joao, ana)` returns nil (the stale-client case).
  - `RemoveFollower(joao, ana)` leaves `Profile(ana,"joao").Relationship.Following == ""`.
  - Following again gives `StatusAccepted`.
- [ ] **Step 2: Run** `GOCACHE=$(mktemp -d) go test ./internal/social/ -run TestFollowIsInstant`. Expected: FAIL on status `pending`.
- [ ] **Step 3: Implement.**
  - `Follow` passes `StatusAccepted` and notes `KindNewFollower` with the Global Constraints copy when `before == ""`.
  - `Accept` checks `repo.FollowStatus(follower, followee)`: accepted means nil, empty means `ErrNotFound`, otherwise the existing accept.
  - `Overview` puts every follower into `Followers`.
  - Update the doc comments on `Follow`, `RemoveFollower` and `Unfollow` so they no longer talk about requests or withdrawing.
- [ ] **Step 4: Run** `GOCACHE=$(mktemp -d) go test ./internal/social/...`. Expected: PASS, including `api_contract_test.go`. If the contract fixture pins `pending`, update it to `accepted`.
- [ ] **Step 5: Commit** `feat(social): follows take effect at once`.

### Task 4: Web Friends page without requests; sharing anchor; copy in four locales

**Files:**
- Modify: `web/social/social.templ`
  - Remove the requests block (`:114-140`).
  - Simplify `unfollowLabel` (`:297`) so it always returns `social.unfollow`.
  - Drop the pending badge in `personRow` (`:315`).
  - Add `id="sharing"` to the root of `sharingCard` (`:439`).
- Modify: `internal/shared/i18n/{en,es,pt_pt,pt_br}_social.go`. Remove `social.requests.title`, `social.waiting` and `social.withdraw` if nothing else uses them. Change any follow-confirmation copy that says "request sent" to "Following" (grep for keys whose text mentions request).
- Regenerate: `task generate`
- Test: `internal/social/handler_test.go`

**Interfaces:**
- Consumes: `Follow` returning accepted (Task 3).

- [ ] **Step 1: Write the failing test** `TestFriendsPageFollowIsInstant`, in the style of the existing handler tests. B has handle `joao`. A posts `/app/friends/follow` with `handle=joao`, then GETs `/app/friends`. The body contains João's name under following, with the Unfollow label. B's `/app/friends` lists A under followers and does not contain the en text of `social.requests.title`. B's page also contains `id="sharing"`.
- [ ] **Step 2: Run** `GOCACHE=$(mktemp -d) go test ./internal/social/ -run TestFriendsPageFollowIsInstant`. Expected: FAIL (no sharing anchor; requests text present before Task 3 data).
- [ ] **Step 3: Implement** the templ and i18n edits, then run `task generate`. Run `go vet ./...` and confirm i18n has no missing-key test failures (`go test ./internal/shared/i18n/`).
- [ ] **Step 4: Run** `GOCACHE=$(mktemp -d) go test ./internal/social/... ./internal/shared/i18n/ ./web/...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): friends page for open follow`.

### Task 5: OpenAPI, full suite, browser check, PR

**Files:**
- Modify: `docs/api/openapi.yaml`:
  - `POST /follows`: the follow is instant and `status` is `accepted`.
  - `/followers/{id}/accept`: deprecated, a 204 no-op when already accepted.
  - Overview `requests`: deprecated, always empty.
  - `GET /social/sharing`: defaults for new people.

  Descriptions only; no enum changes.

- [ ] **Step 1: Edit the spec descriptions.** Run the API contract tests: `GOCACHE=$(mktemp -d) go test ./internal/social/ -run Contract`. Expected: PASS.
- [ ] **Step 2: Run the full suite:** `GOCACHE=$(mktemp -d) TEST_DATABASE_URL=... go test ./...` (memory `local-test-database`). Expected: all PASS.
- [ ] **Step 3: Headless two-context browser check** (memory `north-headless-screenshots`; run `scripts/build-css.sh` first in the worktree):
  - A follows B by handle, and A sees Unfollow at once.
  - B's Friends page lists A under followers with no Accept button, and B's inbox shows "A followed you".
  - B's inbox also shows the "Khepri now has followers" notice, which links to `#sharing`.
- [ ] **Step 4: Commit** `docs(api): open follow semantics`. Push `feat/open-follow` and open the north-client PR. The body lists the migration's irreversible steps (pending→accepted, pinned rows) and the deploy note (memory `north-promote-prs-are-manual`).

### Task 6: iOS: remove requests and pending states (north-ios PR)

**Files** (new worktree off `north-ios-app` origin/main, branch `feat/open-follow`):
- Modify: `khepri/Features/Friends/FriendsScreen.swift`
  - Delete the requests `Section` (`:130-145`).
  - The following row (`:198-201`) always says "Unfollow" and drops the `pending:` argument.
- Modify: `khepri/Features/Friends/FindContactsScreen.swift:175,187`: a successful follow shows Following, and the haptic triggers on `"accepted"`.
- Modify: `PersonRow` (wherever it is defined): remove the `pending` parameter if nothing else uses it.
- Modify: `Localizable.xcstrings`: drop the unused "Accept"/"Decline"/"Withdraw"/waiting strings if they're unreferenced. Add "Following" in en/es/pt-PT/pt-BR if missing.
- Modify: `NorthKit/Sources/NorthAPI/openapi.yaml`: mirror the Task 5 description changes only.
- Test: `khepriTests`. Update any Friends view-model test that asserts requests.

- [ ] **Step 1: Write or adjust the unit test** that pins "follow result `accepted` → row shows Unfollow, no requests section". Use the existing FriendsScreen/FindContacts test seams. If none exist, test the `FindContacts` match-state mapping.
- [ ] **Step 2: Run** `xcodebuild test -scheme khepri -only-testing:khepriTests` (memory `north-ios-ui-testing`; keep lint at 0, memory `ios-lint-blocks-xcodebuild`). Expected: the new test FAILS.
- [ ] **Step 3: Implement** the Swift edits.
- [ ] **Step 4: Run** the same command. Expected: PASS with 0 lint errors. Do a sim check against the local server running Task 3: follow by handle shows Unfollow at once.
- [ ] **Step 5: Commit, push and open the north-ios PR,** which depends on the north-client PR being live. It ships on the beta lane.
