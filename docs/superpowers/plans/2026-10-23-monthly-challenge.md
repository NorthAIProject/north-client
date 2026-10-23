# Monthly Challenge (phase 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One personal challenge per month, auto-picked from the user's own last three months, with a gold limited-edition medal when met — on profiles, the awards page, Progress, the feed and the unlock moment, on web and iOS.

**Architecture:** The challenge lives inside the existing `internal/awards` slice (same sqlc package, same timezone helper, same best-effort `Check` hook): a `monthly_challenges` row is created lazily for the user's current local month the first time anything asks for it, and `Check` (already called after every training, habits and check-in write) re-counts progress and writes one `achievements` row (`kind='monthly_challenge_met'`, `category='awards'`, `source_key='YYYY-MM'`) when the target is reached. Monthly medals travel as `AwardView`s with id `monthly-YYYY-MM` and a new optional `monthly` object, so the existing unseen/celebration, profile and feed paths carry them. The medal is the existing gold scalloped coin plus month and target text, drawn from `docs/medals/medals.json` on both platforms.

**Tech Stack:** Go, Postgres, sqlc v1.31.1, goose, templ, htmx 4, templUI; SwiftUI, swift-openapi-generator.

**Spec:** `docs/superpowers/specs/2026-10-21-follow-awards-design.md` §5 "Monthly challenge" (plus the profile layout and §7 iOS Progress card). Codebase map: session scratchpad `phase4-map.md`; the facts a task needs are repeated in it.

## Global Constraints

- **Metric, fixed at creation, by priority:** `workouts` if the user averaged ≥ 4 completed sessions a month; else `habit_days` if they have an active habit (`habits.active`); else `checkin_days`. Averages are over the last 3 full local months before the challenge month, counting only months the account existed in (min 1 month); no full month yet → starter floor.
- **Target:** `ceil(avg × 1.15)`, capped at `days_in_month − 3`. Starter floor when there is no history: `workouts` 4, `habit_days` 8, `checkin_days` 8. For the two *days* metrics the target is also capped at the days left in the month (today included) when the row is created, so a challenge created on the 25th is reachable. Target ≥ 1 always.
- **Counts:** workouts = `activity_sessions` with `status='completed'`, local month of `ended_at` in `users.timezone` (empty/invalid = UTC); habit days = `count(DISTINCT local_date)` in `habit_completions`; check-in days = `count(*)` of `check_ins` by `local_date`. No XP-style caps.
- **One challenge per user per local month:** `PRIMARY KEY (user_id, month)`; created by `INSERT … ON CONFLICT DO NOTHING` then `SELECT`; never recomputed after creation.
- **Met once:** `achievements` row `kind='monthly_challenge_met'`, `category='awards'`, `source_key='YYYY-MM'`, `private=false`, English title stored, localised at read. `UNIQUE(user_id, kind, source_key)` makes it once-only. No XP (XP reads only `crew_challenge_met`), no push, no nudge.
- **Misses are never shown:** a past month's unmet challenge appears in no response, page or feed. Met months persist as medals.
- **Sharing:** the current challenge on someone else's profile and their monthly medals follow the `awards` switch (`shares.Awards`), like badges. Monthly is never private.
- **Wire:** only new optional fields and new object schemas. `FeedItem.category` stays `training|streaks|goals` (`awards` → `goals` in `projectFeed`, already done); `metric` is an open `string`, never an enum. Every new field and route goes into `docs/api/openapi.yaml` in the same commit.
- **Names:** use the `monthly` prefix everywhere (`monthly_challenges`, `MonthlyChallenge…`, `monthly.*` i18n keys). "Challenge" alone already means crew challenges.
- **Catalog untouched:** monthly is NOT an `award.Catalog` entry and adds nothing under `medals.json` `glyphs` (tests pin 31 badges / 30 locked). Counts like "Earned · N of 31" stay catalog-only.
- **Copy (English binding; es/pt-PT/pt-BR follow the existing `*_awards.go` wording — premio/prémio/prêmio, medalla/medalha, masculine agreement):**
  - `monthly.title` "%[1]s challenge" (full month name) · `monthly.goal.workouts` "Log %[1]d workouts in %[2]s" · `monthly.goal.habit_days` "Keep a habit on %[1]d days in %[2]s" · `monthly.goal.checkin_days` "Check in on %[1]d days in %[2]s"
  - `monthly.progress.workouts` "%[1]d of %[2]d workouts" · `monthly.progress.habit_days` "%[1]d of %[2]d habit days" · `monthly.progress.checkin_days` "%[1]d of %[2]d check-in days"
  - `monthly.met` "Challenge met" · medal name `monthly.medal.name` "%[1]s challenge" (month + year, e.g. "October 2026 challenge") · medal description `monthly.medal.description.<metric>` "Logged %[1]d workouts in %[2]s" / "Kept a habit on %[1]d days in %[2]s" / "Checked in on %[1]d days in %[2]s"
  - No loss framing anywhere: no "days left", "behind", "missed", "only".
- Go tests: one persistent `GOCACHE=<session scratchpad>/gocache`; `TEST_DATABASE_URL=postgres://north:north@localhost:5434/north?sslmode=disable`. iOS: one persistent `-derivedDataPath <session scratchpad>/ios-dd`, `-skipPackagePluginValidation`; SwiftLint 0. No push, PR or merge from implementers. Never `git stash`.

## Review Focus

1. Local month edges: a workout ending 23:30 local on 31 October (already 1 November in UTC) counts for October; the month key is the user's local month. → Task 1 `TestMonthUsesLocalTime`.
2. Two first views racing create one row with one metric/target; a later change in history never changes this month's target. → Task 1 `TestChallengeCreatedOnceAndFixed`.
3. A brand-new user and a user who joins on the 28th both get a reachable target (floor, days-left cap, `days_in_month − 3` cap). → Task 1 `TestTargetFloorsAndCaps` (table).
4. A missed month vanishes: on 1 November an unmet October challenge appears in no API response or page; a met October medal stays everywhere. → Task 2 `TestMissedMonthIsNeverShown`.
5. A follower with the owner's `awards` switch off sees neither the current challenge nor monthly medals; the `seen` route accepts only the caller's own held `monthly-YYYY-MM` ids. → Task 2 `TestMonthlyFollowsAwardsSwitch`, `TestSeenMonthlyOnlyOwn`.

---

### Task 1: Challenge engine (north-client)

**Files:**
- Create: `migrations/<UTC timestamp > 20261023100000>_monthly_challenges.sql`, `internal/awards/monthly.go`, `internal/awards/monthly_test.go`
- Modify: `internal/awards/db/queries.sql` (+ regenerate sqlc), `internal/awards/service.go` (`Check`), `internal/achievements/achievement/achievement.go` (`KindMonthlyChallengeMet = "monthly_challenge_met"`)

**Interfaces:**
- Produces:
  - `type MonthlyMetric string` with `MetricWorkouts = "workouts"`, `MetricHabitDays = "habit_days"`, `MetricCheckinDays = "checkin_days"`
  - `type MonthlyChallenge struct { Month time.Time /* first day, local */; Key string /* "YYYY-MM" */; Metric MonthlyMetric; Target, Progress int; Met bool; MetAt *time.Time }`
  - `(*Service) Monthly(ctx, userID uuid.UUID, now time.Time) (MonthlyChallenge, error)` — get-or-create the current local month's challenge with live progress.
  - `(*Service) MonthlyMedals(ctx, userID uuid.UUID) ([]MonthlyMedal, error)` — met months, newest first; `type MonthlyMedal struct { AchievementID uuid.UUID; Key string; Metric MonthlyMetric; Target int; At time.Time }`
  - `pickMonthly(history monthlyHistory, monthStart time.Time, daysLeft int) (MonthlyMetric, int)` — pure; `monthlyHistory{ Months int; Workouts, HabitDays, CheckinDays int; ActiveHabit bool }`.

Migration: `monthly_challenges(user_id uuid REFERENCES users ON DELETE CASCADE, month date NOT NULL /* first day */, metric text NOT NULL CHECK (metric IN ('workouts','habit_days','checkin_days')), target int NOT NULL CHECK (target >= 1), created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (user_id, month))`. Down drops it and deletes `achievements WHERE kind='monthly_challenge_met'`.

`Check(ctx, userID, at, areas...)`: when areas include Training, Habits or Consistency, after badges, call `checkMonthly(ctx, userID, now)` — `Monthly` then, if `Progress >= Target`, insert the met row (`occurred_at = now`, title `i18n.Translate("en", "monthly.met")`). Best effort: log at Warn, never fail the caller. `at` in an earlier month never grants that month.

- [ ] **Step 1: Failing tests** in `monthly_test.go`:
  - `TestPickMonthly` (table, pure): avg 4.0 workouts → workouts, target 5; avg 3.9 workouts + active habit with 10 habit days avg → habit_days, 12; no habit, 20 check-in days → checkin_days, 23; 30 check-in days in a 30-day month → capped 27; `Months==0` → checkin_days 8 (habit_days 8 if active habit); days metric created with `daysLeft=4` → 4; workouts never capped by daysLeft.
  - `TestMonthUsesLocalTime`: user tz `Europe/Lisbon`; session ending `2026-10-31T23:30` local counts in `2026-10`, not `2026-11`.
  - `TestChallengeCreatedOnceAndFixed`: two goroutines call `Monthly` → one row; add 30 prior sessions → same metric and target next call.
  - `TestTargetFloorsAndCaps`: brand-new user → floor; user created on the 28th of a 31-day month with no history → `checkin_days` target 4.
  - `TestMonthlyMetOnce`: reach target via `Check(Training)` twice → exactly one `monthly_challenge_met` row, `category='awards'`, `private=false`, `source_key` "YYYY-MM"; XP summary unchanged.
  - `TestBackdatedEventNeverGrantsPastMonth`: in November, importing an October session never writes `2026-10`.
- [ ] **Step 2:** run `go test ./internal/awards/ -run 'Monthly|PickMonthly|MonthUses|ChallengeCreated|TargetFloors|Backdated'` → FAIL.
- [ ] **Step 3:** migration, queries (`CreateMonthlyChallenge` ON CONFLICT DO NOTHING, `GetMonthlyChallenge`, `CountMonthlyWorkouts/HabitDays/CheckinDays(user, from, to, tz)`, `MonthlyHistory`, `InsertMonthlyMet`, `ListMonthlyMedals`), sqlc generate, `monthly.go`, hook in `Check`.
- [ ] **Step 4:** tests PASS; `go test ./internal/awards/... ./internal/xp/...`; golangci-lint 0 on changed packages.
- [ ] **Step 5: Commit** `feat(awards): monthly challenge — picked from your last three months, met once`.

### Task 2: Monthly on the wire — awards, profiles, feed, unseen (north-client)

**Files:**
- Modify: `internal/awards/award/view.go` (`View.Monthly *MonthlyView`), `internal/awards/service.go` (`Earned`, `Unseen`, `MarkSeen` include monthly), `internal/awards/db/queries.sql` (unseen/seen for `monthly_challenge_met`), `internal/awards/api.go`, `internal/awards/handler.go` (id validation), `internal/profiles/{service.go,api.go}`, `internal/achievements/{service.go (foldMoments), api.go (FeedItem.Monthly)}`, `internal/shared/i18n/{en,es,pt_pt,pt_br}_awards.go`, `docs/api/openapi.yaml`, goldens (`internal/awards/testdata/{awards,unseen}.golden.json`, new `internal/awards/testdata/awards-monthly.golden.json`, `internal/profiles/testdata/profile-monthly.golden.json`, `internal/achievements/testdata/feed-monthly.golden.json`)

**Interfaces:**
- Consumes: Task 1 `Monthly`, `MonthlyMedals`, `KindMonthlyChallengeMet`.
- Produces (wire, all optional):
  - `MonthlyMedalView { month: "YYYY-MM", metric: string, target: int }`
  - `AwardView.monthly?: MonthlyMedalView` — a monthly medal is an `AwardView` with `id "monthly-YYYY-MM"`, `area "monthly"`, localised `name`/`description`, `earnedAt`, `target`; no `progress`/`rarity`.
  - `MonthlyChallengeView { month: "YYYY-MM", metric: string, target: int, progress: int, met: bool, title: string, goal: string, progressText: string }` (localised strings)
  - `Awards.monthlyChallenge?: MonthlyChallengeView`; `ProfileView.monthlyChallenge?: MonthlyChallengeView` (only when `shares.Awards`); `FeedItem.monthly?: MonthlyMedalView`
  - `award.MonthlyID(key string) string` / `award.ParseMonthlyID(id string) (key string, ok bool)`
- Earned lists (API `/awards`, profile `awards`, web pages) include monthly medals interleaved by `earnedAt`; held/total and locked counts stay catalog-only.
- `/awards/unseen` includes uncelebrated monthly medals; `POST /awards/{id}/seen` and the web batch accept `monthly-YYYY-MM` ids the caller holds (404 otherwise); `celebrated_at` is set on that row.
- `foldMoments`: a `monthly_challenge_met` row gets localised title (`monthly.medal.name`) and detail (`monthly.medal.description.<metric>`) and `Monthly` set from the stored challenge; it is its own card (not merged with badges).

- [ ] **Step 1: Failing tests:** `TestMonthlyMedalInAwardsAndUnseen` (met → in `/awards` earned and `/awards/unseen`; seen → gone from unseen; counts still `held/31`), `TestSeenMonthlyOnlyOwn` (other user's id → 404; malformed → 404), `TestMissedMonthIsNeverShown` (clock in November: no `monthlyChallenge` for October in `/awards` or profile; October met medal still listed), `TestMonthlyFollowsAwardsSwitch` (owner awards off → follower's profile has neither `monthlyChallenge` nor monthly medals; owner sees both), `TestMonthlyFeedCardLocalised` (es viewer gets Spanish title; category `goals`; `monthly` set), golden shape tests for the three new goldens; `cmd/web` api spec test covers routes.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** implement; add i18n keys in all four locales; openapi schemas; `-update` goldens.
- [ ] **Step 4:** full `go test -p 4 ./...` → exit 0; golangci-lint 0.
- [ ] **Step 5: Commit** `feat(awards): monthly challenge and medals in awards, profiles, feed and unseen`.

### Task 3: Monthly coin (north-client)

**Files:**
- Modify: `docs/medals/medals.json` (`monthly` gains `fill`, `text {color, monthSize, monthY, numberSize, numberY, fontWeight}`), `docs/medals/medals.go`, `web/shared/medal/medal.templ` (+ test), `scripts/medals/main.go` (preview section)

**Interfaces:**
- Produces: `medal.MonthlyCoin(p MonthlyProps)` with `MonthlyProps{ Month string /* localised short, upper-case, e.g. "OCT"/"OUT" */; Target int; Size int; Locked bool; Label string }` — gold scallop rim (existing), face `monthly.fill`, no glyph, month text above and target number centred, all geometry from `medals.json`. Locked = same grayscale/opacity as badges.

- [ ] **Step 1: Failing tests** in `medal_test.go`: `TestMonthlyCoinGeometryFromJSON` (fill, text sizes/positions come from the json), `TestMonthlyCoinText` (renders "OCT" and "18"; `Locked` adds the locked style), existing `TestEveryBadgeHasAGlyph` still passes (no glyph entry added).
- [ ] **Step 2:** FAIL → **Step 3:** implement; preview harness shows 3 months (e.g. OCT 18, NOV 5, DEC 27) unlocked and one locked at 96 and 36 px → **Step 4:** PASS, `task medals` renders, screenshot light/dark to the scratchpad.
- [ ] **Step 5: Commit** `feat(medals): monthly coin with month and target`.

### Task 4: Web surfaces (north-client)

**Files:**
- Create: `web/awards/monthly.templ` (`MonthlyCard(c MonthlyChallengeData)`)
- Modify: `web/profiles/profile.templ` (card between header and awards strip), `web/awards/awards.templ` (card at top of own `/app/awards`; monthly medals in Earned use `MonthlyCoin`; `SummaryCard` progress line), `web/awards/unlock.templ` (monthly hero/list coin), `web/social/social.templ` feedCard (monthly branch with `MonthlyCoin`), handlers feeding data (`internal/profiles/handler.go`, `internal/awards/handler.go`, `internal/insights/handler.go`)

**Interfaces:**
- Consumes: Task 2 views; Task 3 `medal.MonthlyCoin`.
- Card: locked `MonthlyCoin` (unlocked when met), `title`, `goal`, a templUI progress bar (`web/shared/ui/progress`) with `progressText`; when met: "Challenge met" and unlocked coin. Shown for owner and, if shared, others.

- [ ] **Step 1: Failing tests:** handler/templ tests — profile shows the card for owner and follower (shared), not when awards switch is off; own awards page and insights card show progress text; feed card for `monthly_challenge_met` renders the monthly coin; unlock modal with a monthly unseen award renders the monthly coin and its seen id posts.
- [ ] **Step 2:** FAIL → **Step 3:** implement → **Step 4:** PASS; full suite exit 0; headless two-account check at 390 and 1280 px (memory `north-headless-screenshots`): A's card shows progress, B sees it on A's profile, A meets target → unlock modal once with gold coin, B sees the feed card; no horizontal scroll at 390 px.
- [ ] **Step 5: Commit** `feat(web): monthly challenge card, medal in feed, awards and unlock`.

### Task 5: iOS monthly coin, sync and celebration (north-ios)

**Files:**
- Modify: `scripts/sync-openapi.sh` run (`NORTH_WEB_APP=<monthly worktree>`), `NorthKit/Sources/NorthKit/Awards/{Medals.swift, CoinMedalView.swift}` (or new `MonthlyCoinView.swift`), `NorthKit/Tests/NorthAPITests/ContractTests.swift` (new goldens `awards-monthly`, `profiles-profile-monthly`, `feed-monthly` decoded + in `covered`), `khepri/Features/Awards/{UnlockCelebrationView.swift, AwardsScreen.swift, AwardDetailSheet.swift, ProfileAwardsScreen.swift}`, `khepri/Features/Friends/FriendsScreen.swift` (monthly feed branch), `Localizable.xcstrings`

**Interfaces:**
- Consumes: Task 2 wire, Task 3 `medals.json`.
- Produces: `MonthlyCoinView(month: String, target: Int, size: CGFloat, locked: Bool)` mirroring `medal.MonthlyCoin` from `medals.json`; a single `AwardCoin(award: Award, size:, locked:)` helper that picks `MonthlyCoinView` when `award.monthly != nil`, else `CoinMedalView` — used in every award list, the detail sheet and the celebration. Month text = localised short month from `monthly.month`.

- [ ] **Step 1: Failing tests:** `MedalsTests` (monthly fill/text decode), `MonthlyCoinTests` (month label per locale: "OCT" en, "OUT" pt), contract tests decode the three new goldens; `AwardsCenterTests` monthly id flows through refresh/dismiss/markSeen unchanged.
- [ ] **Step 2–4:** FAIL → implement → khepriTests, NorthKit tests, SwiftLint 0, check-translations OK.
- [ ] **Step 5: Commit** `Monthly coin, monthly medals in awards lists, feed and celebration`.

### Task 6: iOS challenge cards (north-ios)

**Files:**
- Modify: `khepri/Features/Awards/AwardsCard.swift` (monthly progress row under the coins, from `Awards.monthlyChallenge`), `khepri/Features/Profile/{ProfileModel.swift, ProfileScreen.swift}` (Section between header and Awards from `ProfileView.monthlyChallenge`), `khepri/Features/Awards/AwardsScreen.swift` (card at top), `Localizable.xcstrings` only if a new static string is needed (title/goal/progress text come localised from the server)

**Interfaces:**
- Consumes: Task 2 `MonthlyChallengeView`, Task 5 `MonthlyCoinView`.

- [ ] **Step 1: Failing tests:** `ProfileScreenModelTests` (monthly shown when present; hidden when absent), `AwardsModelTests` (monthly progress mapping, met state).
- [ ] **Step 2–4:** FAIL → implement → khepriTests, SwiftLint 0; UI test against the local fake-model server: fresh account sees the challenge card on Progress and own profile; meeting the target shows the celebration once with the gold coin.
- [ ] **Step 5: Commit** `Monthly challenge card on Progress, profile and awards`.
