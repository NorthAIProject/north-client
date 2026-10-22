# Profiles + Rich Workout Card Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every person gets a profile (`/app/u/{handle}` on web, `ProfileScreen` on iOS) showing what they share, and every finished workout in the feed becomes a rich card (time, sets, volume, top 2 exercises, PR chips) that opens the workout.

**Architecture:** One pure builder, `lift.BuildCard`, turns a finished `activity_sessions` row plus its `set_logs` into a `lift.Card`. `lifts.Service.CardsFor` batches that for any list of session IDs. The feed attaches a card to each `workout_completed` item, and a new `internal/profiles` slice composes social (person, counts, relationship, blocks), achievements (the owner's sharing switches), xp (level) and the cards into one view for the API, the web page and iOS. Nothing new is stored: cards and PR flags are computed on read, like recaps.

**Tech Stack:** Go, Postgres, sqlc v1.31.1 (`go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`), templ (`GOFLAGS=-mod=mod go tool templ generate`), htmx, templUI (`web/shared/ui`); SwiftUI + swift-openapi-generator on iOS.

**Spec:** `docs/superpowers/specs/2026-10-21-follow-awards-design.md` — sections "2. Profiles", "3. Rich workout feed card", "7. iOS" (profile and feed parts). Awards, the awards strip, the monthly challenge card and locked badges are phases 3–4 and are **out of scope** here.

**Codebase map** (facts, file:line, signatures) used to write this plan: ask the controller for it if a task needs more than its Interfaces block.

## Global Constraints

- Visibility: any signed-in viewer who is not blocked (either direction) sees a profile. A block or an unknown handle is the same `404` — reuse `social.Service.Profile`, which already does this.
- Each profile section is gated by the **owner's** switch from `achievements.Service.Sharing(ctx, ownerID)`: workouts (count, list, detail, cards) by `Training`; level by `XP`. The owner always sees everything of their own.
- A workout is a row in `activity_sessions` with `status = 'completed'`; imports (Strava, Health) and manual logs count. Never show calories, heart rate or body numbers on a card or profile.
- Shipped iOS builds must keep decoding every response: add only **optional** fields and new objects; **no new enum values** (`FeedItem.category`, `EarnedView.kind`, `Leaderboard.metric` stay closed). Never make an existing field optional or remove it.
- Every new `/api/v1` route goes into `docs/api/openapi.yaml` in the same commit (`TestOpenAPISpecMatchesMountedRoutes` enforces it). JSON is camelCase, timestamps RFC3339.
- Web copy lives in four locales: `internal/shared/i18n/{en,es,pt_pt,pt_br}_social.go`, new keys prefixed `profile.` or `workoutcard.`. Every new key goes into all four (pt-PT uses "tu", pt-BR "você", es "tú", matching the existing catalogs).
- Use templUI components from `web/shared/ui/` (card, button, avatar, badge) before hand-rolling markup.
- Feed and card building are best effort: if cards cannot be built, log at Warn and return the feed without them; never fail the feed.
- Go tests use ONE persistent cache: `GOCACHE=<session scratchpad>/gocache` (never `$(mktemp -d)` — that filled the disk). DB tests: `TEST_DATABASE_URL=postgres://north:north@localhost:5434/north?sslmode=disable`.
- No push, PR or merge from implementers; the controller handles integration with the user's approval.

## Review Focus

1. **A blocked person opens a profile, a workout list or a workout URL directly** (either side of the block) → `404`, same as an unknown handle. Test in Task 3.
2. **An existing person who never turned training on** (their row is pinned `false` by the open-follow migration) → a stranger sees no workout count, no list (`shared: false`, empty items) and `404` on a guessed workout URL. Test in Task 3.
3. **A workout URL that pairs one person's handle with another person's session id**, or a session that is not completed → `404`. Test in Task 3.
4. **An imported run with no sets** (Strava/Health) → the card shows time and distance only — never "0 sets" or "0 kg". Tests in Task 1 (card) and Task 4 (render).
5. **A feed item whose session was cancelled or whose `source_key` is not a UUID** → the feed still renders, that item just has no card. Test in Task 2.

---

### Task 1: Workout cards (activity queries + `lift.Card`)

**Files:**
- Modify: `internal/activity/db/queries.sql` (+ regenerate sqlc), `internal/activity/repository.go`, `internal/activity/service.go`
- Modify: `internal/lifts/db/queries.sql` (+ regenerate), `internal/lifts/repository.go`, `internal/lifts/recap.go` (the `Sessions` interface)
- Create: `internal/lifts/lift/card.go`, `internal/lifts/lift/card_test.go`, `internal/lifts/cards.go`, `internal/lifts/cards_test.go`
- Modify: test stubs implementing `lifts.Sessions` (grep for them)

**Interfaces:**
- Produces (activity):
  - sqlc `CompletedSessionsByIDs :many` — `WHERE id = ANY(@ids::uuid[]) AND status = 'completed'`. Not owner-scoped: **callers authorize**; say so in the doc comment.
  - sqlc `ListCompletedSessions :many` — `WHERE user_id = @user_id AND status = 'completed' AND ended_at < @before ORDER BY ended_at DESC, id DESC LIMIT @lim`.
  - sqlc `CountCompletedSessions :one` — `count(*)` of completed sessions for a user.
  - `func (s *Service) CompletedByIDs(ctx context.Context, ids []uuid.UUID) ([]activity.Session, error)`
  - `func (s *Service) ListCompleted(ctx context.Context, userID uuid.UUID, before time.Time, limit int) ([]activity.Session, error)`
  - `func (s *Service) CountCompleted(ctx context.Context, userID uuid.UUID) (int, error)`
- Produces (lift, pure):
  ```go
  type CardSession struct {
      ID           uuid.UUID
      UserID       uuid.UUID
      Name         string        // MET name, e.g. "Weight training"
      ActivityCode string
      StartedAt    time.Time
      EndedAt      time.Time
      Duration     time.Duration // session.Elapsed(EndedAt), the same figure WorkoutCompleted uses
      DistanceM    *float64
  }
  type CardExercise struct {
      Name         string
      Sets         int     // working sets
      VolumeKg     float64
      Best         string  // English "22.5 kg × 8"; "" for bodyweight-only
      BestWeightKg float64
      BestReps     int
      PR           bool
  }
  type Card struct {
      CardSession
      Sets      int
      VolumeKg  float64
      PRs       int            // exercises with PR == true
      Exercises []CardExercise // volume desc, then name asc
  }
  func BuildCard(s CardSession, current, earlier []Set) Card
  func (e CardExercise) BestIn(tag string) string // same rule as RecapExercise.BestIn
  ```
- Produces (lifts service): `func (s *Service) CardsFor(ctx context.Context, sessionIDs []uuid.UUID) (map[uuid.UUID]lift.Card, error)` — keyed by session id; sessions that are missing or not completed are simply absent.
- Consumes: `lift.Working`, `Set.Volume`, `Set.E1RM`, `lift.Records` (`internal/lifts/lift/lift.go`), the "heaviest set" rule and `formatKg` from `internal/lifts/lift/recap.go`; `activity.LookupMET(code).Name`; repo `ListForExercisesBefore`.

**Rules `BuildCard` must follow:**
- Only working sets count (`lift.Working`): sets, volume and best set all ignore warmups.
- An exercise's `PR` is true when `lift.Records(earlier ++ current)` returns a record whose set is one of `current` — the same definition the lifts stats page uses, so the first time someone does an exercise is a baseline, not a PR. `earlier` is that person's sets for the same exercises in the 365 days before `StartedAt`, oldest first.
- No sets → `Sets == 0`, `VolumeKg == 0`, `Exercises` empty (not nil-vs-empty sensitive in Go, but the API must emit `[]`).

**`CardsFor` algorithm** (keep it to a fixed number of queries per owner, not per session):
1. `CompletedByIDs(ids)`.
2. One new sqlc query `ListSetsForSessions :many` — `WHERE activity_session_id = ANY(@ids::uuid[]) ORDER BY performed_at, set_number`.
3. Per distinct owner: one `ListForExercisesBefore(owner, since = earliest StartedAt − 365 days, before = latest StartedAt, keys = that owner's exercise keys)`; then, per session, filter that history to `[StartedAt − 365d, StartedAt)`.
4. `BuildCard` each session.

The `Sessions` interface in `internal/lifts/recap.go` gains `CompletedByIDs(ctx context.Context, ids []uuid.UUID) ([]activity.Session, error)`; `*activity.Service` already satisfies it after this task. Update any test stubs.

- [ ] **Step 1: Write the failing pure tests** in `internal/lifts/lift/card_test.go`:

```go
func TestCardCountsWorkingSetsAndOrdersByVolume(t *testing.T) {
    // squat 3×5 @100 (1 warmup @60 ignored), bench 3×8 @60
    c := lift.BuildCard(session(45*time.Minute), currentSets, nil)
    // c.Sets == 6, c.VolumeKg == 3*5*100 + 3*8*60 == 2940
    // c.Exercises[0].Name == "Squat", c.Exercises[0].Best == "100 kg × 5"
    // c.PRs == 0 (no history: first time is a baseline)
}
func TestCardFlagsAPROnlyAgainstEarlierSets(t *testing.T) {
    // earlier: squat 100×5; current: squat 105×5 → Squat.PR == true, c.PRs == 1
    // earlier: squat 110×5; current: squat 105×5 → PR == false
}
func TestCardForARunWithNoSets(t *testing.T) {
    // DistanceM 5000, no sets → Sets 0, VolumeKg 0, len(Exercises) == 0, Duration kept
}
func TestCardBestInUsesTheReadersDecimalMark(t *testing.T) {
    // BestWeightKg 22.5, reps 8 → BestIn("es") == "22,5 kg × 8"
}
```

- [ ] **Step 2: Run** `go test ./internal/lifts/lift/ -run Card` → FAIL (`undefined: lift.BuildCard`).
- [ ] **Step 3: Implement** `lift.BuildCard` and `CardExercise.BestIn` in `internal/lifts/lift/card.go`, reusing the recap's helpers rather than copying them.
- [ ] **Step 4: Write the failing DB test** `TestCardsForBatchesAcrossPeople` in `internal/lifts/cards_test.go`: two people, three completed sessions with sets plus one cancelled session; `CardsFor` of all four ids returns exactly the three completed ones, each with its own sets, and a PR flag where that person's earlier set was lighter. Also `TestCountAndListCompleted` in `internal/activity` (count excludes cancelled/active; list is `ended_at` desc and honours `before`).
- [ ] **Step 5: Add the sqlc queries, regenerate, implement** the activity methods and `lifts.Service.CardsFor`. Run `go test ./internal/activity/... ./internal/lifts/...` → PASS.
- [ ] **Step 6: Commit** `feat(lifts): workout cards built from a session and its sets`.

---

### Task 2: Rich workout card in the feed (API + web)

**Files:**
- Modify: `internal/achievements/db/queries.sql` (`Feed` selects `a.source_key`; regenerate), `internal/achievements/achievement/achievement.go` (`Item`), `internal/achievements/service.go`, `internal/achievements/api.go`, `internal/achievements/api_contract_test.go`, `internal/achievements/testdata/feed.golden.json`
- Modify: `internal/lifts/api.go` (new exported DTO + projector)
- Create: `web/shared/workoutcard/workoutcard.templ`, `web/shared/workoutcard/workoutcard_test.go`
- Modify: `web/social/social.templ` (`feedCard` uses the shared card for workout items), `cmd/web/routes.go` (wiring), `docs/api/openapi.yaml`, the four `*_social.go` catalogs

**Interfaces:**
- Consumes: `lifts.Service.CardsFor`, `lift.Card` (Task 1).
- Produces:
  - `achievement.Item` gains `SourceKey string` and `Workout *lift.Card`.
  - In `achievements`: `type WorkoutCards interface { CardsFor(ctx context.Context, sessionIDs []uuid.UUID) (map[uuid.UUID]lift.Card, error) }` and `func (s *Service) WithWorkoutCards(w WorkoutCards) *Service`, following the existing `With…` style. Wire it in `cmd/web/routes.go` once `liftSvc` exists. (`achievementSvc` is built before `liftSvc`, so set it after `liftSvc` is constructed; match how `WithInbox` is applied at `routes.go:199`.)
  - `Service.Feed` attaches a card to each `workout_completed` item whose `source_key` parses as a UUID and is present in the map. Any error → `s.log.Warn(...)` and the items are returned without cards.
  - In `internal/lifts/api.go`:
    ```go
    type WorkoutCardView struct {
        SessionID       uuid.UUID              `json:"sessionId"`
        Name            string                 `json:"name"`
        ActivityCode    string                 `json:"activityCode"`
        StartedAt       time.Time              `json:"startedAt"`
        EndedAt         time.Time              `json:"endedAt"`
        DurationSeconds int                    `json:"durationSeconds"`
        DistanceM       *float64               `json:"distanceM,omitempty"`
        Sets            int                    `json:"sets"`
        VolumeKg        float64                `json:"volumeKg"`
        PRs             int                    `json:"prs"`
        Exercises       []WorkoutExerciseView  `json:"exercises"`
    }
    type WorkoutExerciseView struct {
        Name     string  `json:"name"`
        Sets     int     `json:"sets"`
        VolumeKg float64 `json:"volumeKg"`
        Best     string  `json:"best,omitempty"`
        PR       bool    `json:"pr"`
    }
    // ProjectCard keeps the first limit exercises (limit <= 0 keeps all) and
    // always emits exercises as [] rather than null.
    func ProjectCard(c lift.Card, limit int) WorkoutCardView
    ```
  - `FeedItem` gains `Workout *lifts.WorkoutCardView `json:"workout,omitempty"`` built with `ProjectCard(card, 2)`.
  - OpenAPI: new schemas `WorkoutCard` and `WorkoutExercise`; `FeedItem.workout` is an optional `$ref: WorkoutCard`. Nothing becomes required on `FeedItem`.
  - Web: `templ Card(c lift.Card, href string)` in `web/shared/workoutcard`. It renders:
    - the stats row as time, then sets, then volume; a part that is zero is left out, and distance is shown when present;
    - the top 2 exercises with `BestIn(locale)`;
    - one PR chip per PR exercise.
    The whole card links to `href`, which is the profile workout page (Task 4) for the item's author, built as `/app/u/{handle}/workouts/{sessionId}`. Use no link when the author has no handle.
  - i18n keys (all four locales): `workoutcard.sets` ("%d sets"), `workoutcard.set` ("1 set"), `workoutcard.volume` ("%s kg"), `workoutcard.minutes` ("%d min"), `workoutcard.km` ("%s km"), `workoutcard.pr` ("PR").

- [ ] **Step 1: Write failing tests:**
  - `TestFeedAttachesWorkoutCards` (achievements, DB). Ana follows Leo, who shares training. Leo finishes a session with sets. `Feed(ana)` returns the item, and its `Workout` has the right `Sets` and `VolumeKg`.
  - `TestFeedSurvivesABrokenWorkoutReference` (Review Focus 5). An item whose `source_key` is `"not-a-uuid"`, plus an item whose session was cancelled, both come back with `Workout == nil`, and `Feed` returns no error.
  - Golden `feed.golden.json` gains one item with a `workout` object, holding 2 exercises and one with `pr: true`. A second item has no `workout` key at all.
  - `TestWorkoutCardOmitsZeroParts` in `web/shared/workoutcard` (Review Focus 4). Render a run with no sets and 5000 m. The output contains "5 km" and the minutes, and contains neither "0 sets" nor "0 kg".
- [ ] **Step 2: Run** the three packages → FAIL.
- [ ] **Step 3: Implement** (query column, Item fields, `WithWorkoutCards`, Feed enrichment, DTO + `ProjectCard`, FeedItem field, templ card, `feedCard` switch on `it.Workout != nil`, wiring, OpenAPI, copy). Update golden with `go test ./internal/achievements -run TestFeedShapes -update` and read the diff.
- [ ] **Step 4: Run** `go test ./internal/achievements/... ./internal/lifts/... ./web/... ./cmd/web/...` → PASS (includes the OpenAPI/route guard).
- [ ] **Step 5: Commit** `feat(feed): rich workout card with stats, top exercises and PRs`.

---

### Task 3: `internal/profiles` service + API

**Files:**
- Create: `internal/profiles/service.go`, `internal/profiles/api.go`, `internal/profiles/service_test.go`, `internal/profiles/api_contract_test.go`, `internal/profiles/testdata/profile.golden.json`, `internal/profiles/testdata/profile-workouts.golden.json`
- Modify: `internal/xp/service.go` (+ test), `internal/xp/api.go` (export the level projector), `internal/social/api.go` (move `GET /profiles/{handle}` out — see below), `cmd/web/api.go`, `cmd/web/routes.go`, `docs/api/openapi.yaml`, iOS-facing golden list note in the report

**Interfaces:**
- Consumes: `social.Service.Profile(ctx, viewerID, handle) (social.Profile, error)` (404 on unknown or blocked), `achievements.Service.Sharing(ctx, ownerID) (achievements.Sharing, error)`, `activity.Service.CountCompleted / ListCompleted / CompletedByIDs`, `lifts.Service.CardsFor`, `lifts.ProjectCard` (Tasks 1–2).
- Produces in xp: `func (s *Service) LevelOf(ctx context.Context, userID uuid.UUID, now time.Time) (Level, error)` gives lifetime XP → `LevelFor`, computed in UTC. Also `func ProjectLevel(ctx context.Context, l Level) LevelView`, the existing `projectLevel` renamed.
- Produces in profiles. Each dependency is a small interface declared in `service.go`, so tests can stub when useful:
  ```go
  type Shares struct{ Training, XP bool } // what this viewer may see
  type View struct {
      Profile  social.Profile // person, follower/following counts, relationship
      Shares   Shares
      Level    *xp.Level     // nil unless Shares.XP
      Workouts *int          // completed count; nil unless Shares.Training
      Recent   []lift.Card   // newest 5; nil unless Shares.Training
  }
  type Page struct {
      Items  []lift.Card
      Before *time.Time // EndedAt of the last item when a full page came back
      Shared bool
  }
  func NewService(people People, sharing Sharing, levels Levels, sessions Sessions, cards Cards) *Service
  func (s *Service) View(ctx context.Context, viewer users.User, handle string, now time.Time) (View, error)
  func (s *Service) Workouts(ctx context.Context, viewer users.User, handle string, before time.Time) (Page, error) // page size 20; zero before = now
  func (s *Service) Workout(ctx context.Context, viewer users.User, handle string, sessionID uuid.UUID) (lift.Card, error)
  ```
  `Shares` for the owner viewing themself is `{true, true}`. `Workout` returns `apperr.ErrNotFound` in any of these cases: training is not shared, the session isn't completed, or it belongs to someone other than the handle's person.
- API (profiles), all behind `RequireBearer`:
  - `GET /profiles/{handle}` moves here from `internal/social/api.go`. The response stays a strict superset of today's `ProfileView` and keeps every existing field and its JSON name. It adds these optional fields:
    - `shares` `{training, xp}`;
    - `level` (`LevelView`), only when shared;
    - `workouts` (int), only when shared;
    - `recentWorkouts` (`[]WorkoutCardView`, limit 2 exercises each), only when shared.
  - Keep `operationId: getPublicProfile`. Move the golden: `internal/social/testdata/public-profile.golden.json` stays as is, and the new `profile.golden.json` holds the full shape.
  - `GET /profiles/{handle}/workouts?before=RFC3339` returns `{items: []WorkoutCardView (limit 2), before?: string, shared: bool}`. Its operationId is `listProfileWorkouts`.
  - `GET /profiles/{handle}/workouts/{sessionID}` returns a `WorkoutCardView` with all exercises. Its operationId is `getProfileWorkout`. An unparseable ID returns 404.
  - Errors use `httpx.Error(w, err, "Nobody has that handle.")` for profile and list, and `"Not found."` for the detail.

- [ ] **Step 1: Write failing DB tests** in `internal/profiles/service_test.go`:
  - `TestProfileShowsWhatTheOwnerShares`: Leo is new and shares training by default; his `xp` is off. A stranger (not a follower) sees `Workouts` = 2, `Recent` with 2 cards, and `Level == nil`. Once Leo turns xp on, `Level` is set.
  - `TestPinnedOffOwnerShowsNoWorkouts` (Review Focus 2): Leo's training sharing is set false. Ana's `View` has `Workouts == nil` and `Recent == nil`. `Workouts(ana, "leo")` returns `{Shared:false, Items: empty}`. `Workout(ana, "leo", leoSession)` returns `ErrNotFound`.
  - `TestOwnProfileShowsEverything`: with all of Leo's sharing off, Leo still sees his own count, recent workouts and level.
  - `TestBlockedPeopleGetNotFound` (Review Focus 1): blocks in both directions return `ErrNotFound` from `View`, `Workouts` and `Workout`.
  - `TestWorkoutMustBelongToTheHandle` (Review Focus 3): `Workout(ana, "leo", zoeSession)` returns `ErrNotFound`. A cancelled session of Leo's returns `ErrNotFound`.
  - `TestWorkoutsPageByEndedAt`: 23 sessions return a first page of 20 with `Before` set, then a second page of 3 with `Before == nil`.
  - `TestLevelOf` (xp): lifetime points map to the same level as `Summary`.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement the service and xp additions.** Run the tests → PASS.
- [ ] **Step 4: Write the API contract test** `TestProfileShapes` (goldens for both shapes). Implement `api.go`, mount it in `cmd/web/api.go`, construct it in `routes.go`, remove the old route from `social/api.go`, and update the OpenAPI for all three routes plus the new `ProfileView` properties (all optional) and `ProfileWorkouts`. Generate goldens with `-update` and read them.
- [ ] **Step 5: Run** `go test ./internal/profiles/... ./internal/social/... ./internal/xp/... ./cmd/web/...` → PASS.
- [ ] **Step 6: Commit** `feat(profiles): profile with shared workouts and level, workout list and detail`.

---

### Task 4: Web profile page, workout page, links

**Files:**
- Create: `internal/profiles/handler.go`, `internal/profiles/handler_test.go`, `web/profiles/profile.templ`, `web/profiles/workout.templ`, `web/profiles/i18n_test.go`
- Modify: `cmd/web/routes.go` (mount `profilesHandler.Routes(r)` inside the onboarded `/app` group next to `socialHandler.Routes(r)`), `web/social/social.templ` (`personRow` name and `feedCard` author link to the profile; "Your profile" link beside the handle form when the viewer has a handle), `web/leaderboard/leaderboard.templ` (`row` name links when `Handle != ""`), the four `*_social.go` catalogs

**Interfaces:**
- Consumes: `profiles.Service` (Task 3), `workoutcard.Card` (Task 2), `social.Service.Follow(ctx, followerID, handle)`, `social.Service.Unfollow(ctx, followerID, followeeID)`, `xp.ProjectLevel`.
- Produces web routes (relative to `/app`):
  - `GET /u/{handle}`: the profile page.
  - `GET /u/{handle}/workouts?before=`: the full list, with a "More" link carrying `before`.
  - `GET /u/{handle}/workouts/{sessionID}`: one workout, with all exercises and their PR chips.
  - `POST /u/{handle}/follow` and `POST /u/{handle}/unfollow`: these return 303 to `/app/u/{handle}`. Use CSRF-protected `form` posts the way the Friends page does.
  - Unknown, blocked or hidden pages return 404 through the same `fail` idiom as `internal/social/handler.go`.
- Page layout is one scroll, in this order:
  1. Header: initials avatar, name, `@handle`, and "Level N · Title" when `Level != nil`.
  2. Counts row: workouts, then followers, then following. Leave out the workouts count when it is nil.
  3. The Follow or Unfollow button. Hide it on your own page.
  4. A "Workouts" section holding the recent cards plus a "See all" link. When workouts are not shared, show the line "Workouts aren't shared." instead. When shared but empty, show "No workouts yet."
- Nav: `layout.BuildNav(ctx, "/app/friends")`.
- i18n keys (all four locales):
  - `profile.workouts` "Workouts"
  - `profile.followers` "Followers"
  - `profile.following` "Following"
  - `profile.seeall` "See all"
  - `profile.more` "More"
  - `profile.hidden` "Workouts aren't shared."
  - `profile.empty` "No workouts yet."
  - `profile.yours` "Your profile"
  - `profile.level` "Level %d · %s"
  - Reuse `social.unfollow` and `social.follow.ask` for the buttons.

- [ ] **Step 1: Write failing handler tests** (`internal/profiles/handler_test.go`, real DB, router built as in `internal/social/handler_test.go`, user in context via `auth.ContextWithUser`):
  - `TestProfilePageShowsSharedWorkouts`: Ana opens `/u/leo` and gets 200. The page contains "Leo", "@leo" and the session's MET name, and a link to `/app/u/leo/workouts/<id>`.
  - `TestProfilePageHidesUnsharedWorkouts`: with Leo's training off, the page contains "Workouts aren't shared." and no workout link. A GET of `/u/leo/workouts/<id>` returns 404.
  - `TestProfilePageFollowIsInstant`: POST `/u/leo/follow` returns 303. The page then shows "Unfollow", and POST `/u/leo/unfollow` brings back "Follow".
  - `TestBlockedProfilePageIs404`.
  - `TestRunCardHasNoZeroSets` (Review Focus 4): an imported run with no sets shows no "0 sets".
  - `web/profiles/i18n_test.go` asserts that no English strings leak into a pt-PT, pt-BR or es render. Model it on `web/social/i18n_test.go`.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement the handler, the templates, the routes, the name links and the copy.** Run `scripts/build-css.sh` if new Tailwind classes are added.
- [ ] **Step 4: Run** `go test ./internal/profiles/... ./web/... ./cmd/web/... ./internal/social/...` → PASS.
- [ ] **Step 5: Commit** `feat(web): profile page at /app/u/{handle} with workouts and follow`.

---

### Task 5: Full suite + browser check (controller-run)

- [ ] **Step 1:** Run `go test ./...` with the persistent GOCACHE. If OrbStack stops mid-run (mass `SQLSTATE 57P03`), run `orb start`, then `docker start north-client-postgres-1`, then rerun. Expected: all packages pass.
- [ ] **Step 2: Headless two-context browser check.** Follow the memory notes `north-headless-screenshots` and run `scripts/build-css.sh` first.
  - Leo logs a workout with sets. Ana follows Leo.
  - Ana's feed shows the rich card, with the stats row, 2 exercises and a PR chip where expected.
  - Tapping the card opens `/app/u/leo/workouts/<id>`.
  - Ana opens `/app/u/leo` and sees the counts and the recent workouts.
  - Leo turns training off. Ana now sees "Workouts aren't shared." and gets a 404 on the old workout URL.
  - Screenshot the profile at 390 px and 1280 px wide, for the user's verdict.
- [ ] **Step 3:** Ledger the results. Push and the PR wait for the user, via finishing-a-development-branch.

---

### Task 6: iOS — ProfileScreen, rich feed card, workout detail (north-ios PR)

**Files** (new worktree off north-ios `origin/main`, branch `feat/profiles`):
- Run: `scripts/sync-openapi.sh`. It copies `docs/api/openapi.yaml` and the goldens, including the new `internal/profiles/testdata`. If the script lists golden packages, add `profiles` to `GOLDEN_SOURCES`. Then confirm the contract tests decode `profile.golden.json`, `profile-workouts.golden.json` and the new `feed.golden.json`.
- Create:
  - `khepri/Features/Profile/ProfileService.swift`: a `ProfileServicing` protocol plus an implementation calling `getPublicProfile`, `listProfileWorkouts` and `getProfileWorkout`.
  - `khepri/Features/Profile/ProfileScreen.swift`.
  - `khepri/Features/Profile/ProfileWorkoutsScreen.swift`.
  - `khepri/Features/Profile/WorkoutDetailScreen.swift`.
  - `khepri/Features/Profile/WorkoutCardView.swift`: one card view shared by the feed and the profile.
  - `khepri/Features/Profile/WorkoutCardFormat.swift`: pure formatting.
- Modify:
  - `khepri/Features/Friends/FriendsScreen.swift`:
    - `FeedRow` uses `WorkoutCardView` when `item.workout != nil`.
    - The author's name, and the `PersonRow`s in the Following and Followers lists, push `ProfileScreen(handle:)` through a `NavigationLink`, when a handle exists.
    - Add a "Your Profile" row when the viewer has a handle.
  - `Localizable.xcstrings`: en, es, pt-PT and pt-BR for every new string.
  - Tests in `khepriTests`.

**Behaviour:**
- `ProfileScreen` uses the same order and the same hidden and empty copy as Task 4. Follow and unfollow call the existing `FriendsService`.
- `WorkoutCardView` shows the stats row, leaving out zero parts, then the top 2 exercises, then PR chips. Tapping it pushes `WorkoutDetailScreen`.
- Use `.task` loading with an error state. Use no new tabs. The Progress-tab entry comes in phase 3.

- [ ] **Step 1: Write failing unit tests.**
  - `WorkoutCardFormatTests`:
    - a stats line for a lifting card;
    - a run with no sets, showing distance and minutes only;
    - the decimal mark, so `es` shows "22,5 kg".
  - `ProfileScreenModelTests`: hidden versus empty versus list states, built from decoded golden JSON.
- [ ] **Step 2: Run** `xcodebuild test -scheme khepri -only-testing:khepriTests` → FAIL. Read the memories `north-ios-ui-testing` and `ios-lint-blocks-xcodebuild` first. Put DerivedData in one persistent scratchpad dir.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the tests → PASS, with 0 SwiftLint errors. Then run `build-for-testing` for `khepriUITests`, and update any UI test whose labels changed.
- [ ] **Step 5: Commit locally.** The PR waits for the user, and it merges only after the north-client PR is live in prod.
