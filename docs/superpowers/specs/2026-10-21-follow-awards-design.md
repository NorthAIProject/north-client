# Khepri: open following + awards (web + iOS)

## Context

Khepri already has a consent-based follow graph (`internal/social`), an achievements feed with kudos (`internal/achievements`), and XP, levels and a friends-only leaderboard computed on read (`internal/xp`). It has **no badges**, **no unlock moment**, **no profile page on web**, and **no achievements screen on iOS**.

The user wants one loop that brings people back, made of three moments:
1. **A friend did something.** You see their workout, PR or new award, cheer it, and feel pulled to train.
2. **You earned an award.** The app celebrates it in-app, and you want the next one.
3. **Profiles.** You visit a friend's profile, see their awards and workouts, and compare with your own.

The model is Apple Fitness awards (coin medals, monthly limited-edition challenges) plus Strava-style open following.

Strangers-count is below 10, but the user already chose to build social features (memory `khepri-friend-finding-decisions`), so this is not re-raised.

## Decisions (the user's, 2026-10-21)

**Following**
- **Open follow.** A follow takes effect at once, with no approval step (Strava/Instagram-public).
- **Followers see:** achievements and awards, plus workouts (type, duration, sets, top lifts, PRs).
- **Followers never see:** body numbers, meals, sleep figures, medications or mood.

**Feed**
- **Every completed workout** is a feed card, and the card is the **rich** layout: a stats row (time, sets, volume), the top 2 exercises, and PR chips.

**Awards**
- **Earned awards auto-post** to followers' feeds. A sharing switch turns this off. The `awards` switch is **ON for everyone, existing users included**, and they can turn it off.
- **Private-area badges never leave your own profile.** Badges from sleep, nutrition, fasting, mind and body/data are not posted to the feed and are not shown on your profile to others. Training, lifts, consistency, habits, goals, social and monthly badges are shared.
- **Pre-launch history counts, but an award is only earned on the next qualifying event.** Example: a user with 57 workouts logs one more and unlocks First, 10 and 50 together, as one celebration and one feed card. Nothing is bulk-awarded at deploy.
- **Sept gamification rules kept** (`docs/advanced-gamification.md`):
  - Each badge is earned **once**, with **no tiers**.
  - **No push** on unlock.
  - **No retroactive awards**: no bulk award at launch.
  - No badges in nudges or in unprompted coach replies.
  - No body-number badges or boards.
  - No loss-framed copy.
- **Catalog:** about 30 badges spread across the whole app; the 34-badge draft below was approved "as a start". This lifts the old cap of 12.
- **Monthly challenge in v1:** one personal challenge per month, with a limited-edition medal. The metric is **auto-picked per user** from their main activity over the last 3 months (workouts, check-in days or habit days). The target is their own average plus 10–20%. A miss is never mentioned.
- **Medals:** coded vector **coin** style (mockup option A). It has a metallic rim, a deep area colour, a white glyph, and a gold scalloped rim for monthly medals. The same design is drawn as SVG on web and in SwiftUI on iOS.
- **Unlock moment** is in-app only:
  - The medal springs in with a slow 3D turn, plus a haptic on iOS.
  - It shows the award name, why it was earned, the earned date, "Earned by N% of Khepri members", and buttons "View on profile" / "Done".
  - It appears right after the action that earned it, or on the next app open if the award was earned in the background.
- **Rarity %** is shown.

**Screens**
- **Profile layout:** a single scroll (mockup option A). From top to bottom:
  - Header: avatar, name, @handle, level.
  - Counts: workouts · awards · followers · following.
  - The Follow button.
  - A card for the current monthly challenge.
  - An awards strip with "See all".
  - The workout list.
- **Navigation:**
  - iOS: the **Progress tab** gets an Awards + monthly challenge card that opens your own profile. Friends (feed, follow, leaderboard) **stays under More**. No tab changes.
  - Web mirrors this.

Mockups (coin medals, profile, feed card, unlock) were shown in the brainstorming session and are not kept; the coin medal geometry is option A as described above.

## Badge catalog (approved draft, 34)

| Area (coin colour) | Badges |
|---|---|
| Training (orange) | First Workout · 10 · 50 · 100 Workouts · Early Bird (10 sessions started before 07:00 local) · Listened to Your Body (took a lighter day on low recovery) |
| Lifts (amber) | First PR · 10 PRs · Century (first set ≥100 kg, any exercise) |
| Consistency (rose) | 7 · 30 · 100 · 365-day check-in streak |
| Habits (green) | Habit Locked In (one habit kept 4 weeks) · 100 Habit Check-offs |
| Nutrition (lime) | Week of Logging (meals logged 7 days in a row) · Protein on Point (protein target hit on 10 days) |
| Hydration (cyan) | Water Week (goal hit 7 days in a row) · Well Watered (goal hit on 30 days) |
| Sleep (indigo) | Rested Week (7 nights of 7h+ in one week) · 30 Nights Logged |
| Mind (lavender) | First Journal Entry · 10 Journal Entries |
| Goals (emerald) | First Milestone · Goal On Time · 8 Weekly Reviews |
| Body & data (slate) | 4 Progress Checks on cadence · Connected 30 Days (Apple Health/Strava) · 10 Fasts |
| Social (violet) | First Follower · Crew Challenge Met |

The user keeps the verdict on names, copy and medal art in the preview harness.

## Design: how it's built

### 1. Open follow (`internal/social`)
- `Follow(handle)` inserts a row with `status='accepted'` directly.
- A migration accepts all existing `pending` rows.
- The `/followers/{id}/accept` endpoint stays as a no-op for shipped iOS builds.
- The `follow_request` notification becomes `new_follower`: an in-app note plus the existing push path. It is a follower push, not an award push.
- **Already built, so reuse it:**
  - Followers/following lists live in `Overview` (`internal/social/service.go:232-262`).
  - Remove-follower is `DELETE /followers/{userID}` and `/app/friends/{userID}/remove` (`internal/social/handler.go:70`).
  - Only the "Requests" UI goes; the lists get a cleaner layout. Block stays as it is.
- **Defaults for new users:** `training`, `streaks`, `goals` and `awards` are ON; `xp` stays OFF.
- **Existing users:** a migration sets `awards`=ON for everyone (the user's decision); their other switches are unchanged. They see a one-time notice on Friends, "Khepri now has followers + awards — here's what you share", with links to the switches. *(Builder's call: the wording.)*
- **`SetSharing` must not reset `awards` when it's left out.** Shipped iOS saves all switches at once (`internal/achievements/service.go:205-215`), so an omitted field has to keep its stored value. This copies the existing `Sharing.xp` pattern.
- **Leaderboard:** the query already ranks the viewer plus accepted follows who share (`internal/xp/db/queries.sql:116-123`). The only change is that more people can reach it now that following is open.

### 2. Profiles
- **API:** extend `GET /profiles/{handle}` with level, counts, the current monthly challenge, earned awards with earned dates, and recent workouts. Each section is gated by that user's sharing switch.
- **API:** add `GET /profiles/{handle}/workouts` (paginated) and a workout detail endpoint, both gated by `training`.
- **Web:** new page `/app/u/{handle}`. Your own profile is the same page, plus locked badges with progress hints (e.g. "7/10").
- **Visibility:** any signed-in user who isn't blocked sees what the profile owner shares. A follow is instant, so a follow gate would only be cosmetic. *(Claude's call.)*
- **Discovery:** there is still no public directory of strangers. People are found only by handle, contacts, phone or invite.

### 3. Rich workout feed card
- The existing `workout_completed` feed item gains an **open** `workout` summary object: duration, set count, volume, the top 2 exercises with their best set, and PR flags from `internal/lifts`.
- Tapping a card opens the workout detail.

### 4. Badge engine (new `internal/awards` slice, or inside `internal/achievements`; builder's call)
- **Static catalog in Go.** Each entry has an id, area, name, description, glyph and rule.
- **Evaluation runs in two places:**
  - Event-driven checks. Today only activity, checkins, goals, weekly, insights and crews record achievements (e.g. `internal/checkins/service.go:236`, `internal/crews/close_week.go:86`). Lifts PR, habits, meals, hydration, mind, fasting and progresschecks need **new** hooks. Copy the existing best-effort pattern: a failed record is logged and ignored.
  - A nightly sweep for window rules (sleep, hydration, nutrition, connected 30 days).
- **Rules count all history,** but only run when a qualifying event happens after deploy. No deploy-time backfill job.
  - Several thresholds crossed at once give one unlock moment and one feed card listing the badges.
  - *(Claude's call)* Imported or backdated sessions (Strava/Health/manual) count, because history counts. A big import can unlock several badges at once. That's fine and gives one card.
- **What counts as a "workout":** an `activity` session, imports included. Days with sets but no session don't get a feed card. They still count toward the Lifts badges, which read set logs. *(Claude's call)*
- **Storage:** reuse the `achievements` table with `kind='badge_earned'` and `source_key=<badge id>`. The existing `UNIQUE(user_id, kind, source_key)` makes each badge once-only, and kudos and blocks keep working.
  - Migration: widen the `category` CHECK to allow `'awards'` and add `'awards'` to `achievement_sharing`. The feed and kudos queries match `s.category = a.category` (`internal/achievements/db/queries.sql:28-29,48-49`), so `awards` gates badges once the stored category is `awards`.
  - On the wire, `awards` is mapped to the legal closed value `goals` for `FeedItem.category`. New open fields are `badge_id`, `area` and `badges[]`.
  - Add a `private` boolean column to achievements. Badges in the sleep/nutrition/fasting/mind/body areas are written with `private=true`, and the feed and profile queries exclude private rows for anyone but the owner.
- **One card per moment** *(Claude's call)*: when a badge covers the same moment as an existing kind, only the badge posts.
  - Example: a 30-day check-in streak earns the badge the first time. The existing `streak_reached` row is still written, because XP's `streak_mark` derives from it, but it is hidden from the feed when a badge for the same mark was earned in the same event.
  - When a streak is reached again later and there is no new badge, `streak_reached` posts as it does today.
- **XP:** badges and monthly medals grant no XP. *(Claude's call: no double counting.)*
- **Rarity:** a nightly job computes earned / active members (active = any log in the last 90 days), rounds to a whole %, shows "<1%" when below 1, and caches the result per badge. *(Claude's call: the active-member definition.)*
- **Unlock queue:** add `celebrated_at` to achievements.
  - Web: after an htmx action, an `HX-Trigger` opens the celebration modal; on page load, any uncelebrated awards are shown.
  - iOS: fetch `GET /awards/unseen` after a save and on foreground; `POST /awards/{id}/seen` marks it shown.
  - No APNs for awards.

### 5. Monthly challenge
- **Table:** `monthly_challenges(user_id, month, metric, target, created_at)`, created lazily on first view or by a job on the 1st of the month.
- **Metric, by priority** *(Claude's call)*:
  - **workouts**, if the user averaged ≥4 sessions a month over the last 3 months;
  - else **habit days**, if they have an active habit;
  - else **check-in days**.
  - This priority exists because check-ins are near-daily and would win a raw comparison.
- **Target:** `ceil(avg × 1.15)`, capped at `days_in_month − 3`, with a starter floor for users with no history. *(Builder's call: the floor.)*
- **Completion:** meeting the target writes `kind='monthly_challenge_met'` with `source_key='YYYY-MM'`, which posts to the feed.
- **Medal:** gold scalloped rim with the month and target number.
- Past missed months are not shown anywhere.

### 6. Medals: one design, two renderers
- **Web:** a templ SVG component.
- **iOS:** a SwiftUI coin view with the same geometry and colours.
- Each area has its own colour, and each badge has its own glyph.
- **Preview harness** (like `scripts/bodymap/preview`) renders all 34 badges, a monthly medal and the locked state, so the user can give verdicts.

### 7. iOS (`north-ios-app/khepri`)
- **Progress tab:** an Awards card (count, latest 3, monthly progress) that opens `ProfileScreen(self)`.
- **`ProfileScreen`:** works for self and for others; reachable from feed rows and the follower lists.
- **`AwardDetailSheet`.**
- **`UnlockCelebrationView`:** a full-screen cover with a spring animation and a haptic.
- **Friends feed:** the rich workout card and award cards.
- **Followers / Following lists.**
- **Requests UI removed.**
- **OpenAPI:** update both specs (`north-client/docs/api/openapi.yaml` and `NorthKit/.../openapi.yaml`). Add **only open string fields and new objects; never new enum values.** `FeedItem.category`, `EarnedView.kind` and `Leaderboard.metric` are closed in shipped builds (memory `ios-openapi-closed-enums`). Award feed items keep a legal category value and carry `badge_id` / `area` as open fields.

## Phases (one north-client PR, then one north-ios PR, per phase)
0. **Record the design.** Commit this design as `north-client/docs/superpowers/specs/2026-10-21-follow-awards-design.md` and add a dated update to `docs/advanced-gamification.md`: badge cap lifted to about 34, open follow, private-area badges. Then run superpowers:writing-plans for phase 1. Work in worktrees (memory `shared-checkout-branch-switches`).
1. **Open follow:** follow semantics, migration, followers/following lists, defaults, leaderboard, notice.
2. **Profiles + rich workout card:** API, web `/app/u/{handle}`, iOS ProfileScreen, feed card.
3. **Awards:** catalog, engine, nightly sweep, medals (web + iOS + harness), unlock moment, rarity, Progress card, `awards` switch.
4. **Monthly challenge.**

Deploy per memory `north-promote-prs-are-manual` (bump the infra tag by hand). For iOS, use the beta lane; App Store release goes after 1.0.1.

## Verification
- Go: rule tests for every badge, written as table tests against `TEST_DATABASE_URL` (memory `local-test-database`). Use a private `GOCACHE`. Cover once-only, no bulk award at deploy, sharing gates, and block filtering. Monthly target and metric-pick tests.
- Web: a two-context headless browser check (memory `north-headless-screenshots`): A follows B with no approval; B finishes a workout; A sees the rich card and gives kudos; B earns First Workout and sees the modal once; A sees the award on B's profile.
- iOS: UI tests against the local server on the fake model (memory `north-ios-ui-testing`). Run the shipped-build decode check against the new API responses so closed enums hold.
- The user gives verdicts on the medals in the preview harness before phase 3 merges.

## Calls Claude made (check these while reading)
- Profile visible to any signed-in, non-blocked user (follow is instant, so a follow gate is cosmetic); no stranger directory.
- Badges and monthly medals grant no XP.
- Rarity is computed over users active in the last 90 days, rounded, with "<1%" below one percent.
- Imported/backdated sessions count toward badges; set-only days count for Lifts badges but get no feed card.
- One feed card per moment: a badge hides the duplicate `streak_reached` card.
- Monthly metric priority is workouts → habits → check-ins, with the target capped below the month length.
- Badges from private areas (sleep, nutrition, fasting, mind, body) are stored with a `private` flag and never leave the owner's view.
