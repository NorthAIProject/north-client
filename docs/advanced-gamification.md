# Advanced gamification — the full playbook, for a later phase

Written 2026-09-02, alongside the two mechanics that *were* built that day:
the streak-at-risk nudge (`internal/nudges`, kind `streak_at_risk`) and the
moments cards (`internal/moments`, rendered by `web/shared/moment`). This is a
decision record and a starting point, not a build order. **Nothing here is
implemented.**

It exists because the recommendation of the day was "moments and risk, not an
economy", and a recommendation to defer should leave the deferred thing
designed rather than forgotten. When the gate below opens, start here.

---

## The gate

Do not start any section below until all three are true:

| Number | Read from | Threshold |
|---|---|---|
| Streak-at-risk works | `nudge_opened{kind:streak_at_risk}` within the same local day as `nudge_delivered`, vs the same for `missed_checkin` | Evening warning is opened materially more often than the after-the-fact reminder, and the person checks in that day |
| Moments work | week-4 retention of accounts that hit `moment_shown{kind:streak}` at day 3 vs accounts that reached a 3-day streak before moments existed | The cohort with the card retains better |
| There are people | strangers-count from the demand-hour scoreboard | Above the operating rule's threshold (ten), so an A/B has two arms with somebody in each |

If the first two are flat, people are not moved by loss or recognition in this
product, and an economy built on both will be flat too. If the third is not
met, an economy is balanced against nobody.

---

## What survives from the lean version

Three principles were the reason not to build this yet. They do not go away
when it is built; they are its constraints.

1. **Reward outcomes the coach can verify, never the act of logging.** A kept
   habit on its scheduled day, a milestone closed, a training session that
   arrived from Strava, a form check completed. Not "opened the app", not
   "sent a message", not "saved a check-in" — a check-in is how the coach
   learns, and paying for it teaches people to type anything.
2. **Nothing is purchasable.** No freezes, no boosts, no cosmetics for money.
   The moment a mechanic can be bought, the honest signal it was measuring is
   gone. Pro is a tier of capability, not of points.
3. **The `helpful` column stays outside the economy.** It is the one label a
   learned user model cannot derive from behaviour
   (`migrations/20260828180000_feedback_signals.sql`). Rewarding a rating, in
   either direction, poisons it.

And one from `DOMAIN.md`: a ledger is a **log** — append-only, never
"due", aggregated on read. Totals and levels are derived, never stored.

---

## The mechanics

Each section names Duolingo's version, North's version, and the reason for the
difference.

### XP

*Duolingo:* every lesson pays 10–15 XP, bonuses for combos and speed, doubled
in "XP boost" windows.

*North:* a fixed table of verified actions, each paying once per local day per
reference, with diminishing returns on volume so ten water logs are not ten
rewards.

| Action | Verified by | XP | Cap |
|---|---|---|---|
| Habit kept on a scheduled day | `habits.Complete` on a day the schedule names | 10 | one per habit per day |
| Check-in streak day (3+) | `checkins.StreakAt` crossing into a new day | 5 | one per day |
| Training session logged | `activity_sessions` row from Strava sync or the in-app timer, ≥ 10 min | 20 | two per day |
| Form check completed | `media` analysis ready | 15 | one per day |
| Milestone completed | `goals.SetMilestoneStatus` → completed | 40 | per milestone, once |
| Goal achieved | `goals.SetStatus` → achieved | 150 | per goal, once |
| Weekly review read | `reports` opened within 48h of ready | 10 | per report |

Not on the table, deliberately: sending a message, opening the app, reading a
nudge, rating anything, uploading a document, connecting a source (that is the
funnel's job, and the reward for connecting a source is the coach getting
better).

Reversals: a habit un-completed, a milestone reopened, a goal moved back to
active writes a **negative** ledger entry of the same amount with the same
`ref_id`. The ledger stays append-only; the total stays honest.

### Levels

*Duolingo:* levels unlock nothing but a number; leagues are the real ladder.

*North:* levels are **titles** derived from lifetime XP, with no unlocks and
no losses. Seven of them, named for the domain rather than numbered, spaced
so the first two arrive in the first month and the last takes about a year of
real adherence. The coach knows the title through the context builder and can
use it in a sentence; the dashboard shows it under the mascot.

Nothing is gated by level. A gate would make the level a currency.

### Daily goal

*Duolingo:* choose 10/20/30/50 XP per day; the ring fills; the streak counts
days the ring closed.

*North:* one target per day, chosen from what the person actually has —
"check in", "keep every scheduled habit", "one training session" — or the XP
equivalent (15/30/50). Set on the notifications preferences row
(`daily_goal_kind`, `daily_goal_xp`), changed in settings, defaulting to
"check in" because that is the streak the app already keeps.

The ring is the dashboard's `today` panel. It replaces nothing; it sits above
the KPI strip.

**The check-in streak does not become the daily-goal streak.** They are two
numbers. Merging them would turn "I checked in" into "I earned enough", which
is the first step toward paying for logging.

### Streak freeze (rest day)

*Duolingo:* buy a freeze with gems; it auto-applies on a missed day.

*North:* a **rest day** is earned, never bought — one per seven consecutive
check-in days, held to a maximum of two, auto-applied on the first missed
day, and named honestly: the streak line reads "12 days (1 rest day)" rather
than pretending the day happened. `checkins.StreakAt` gains a `rests
[]time.Time` input read from a `streak_rests` log.

A rest day is not a failure and the copy must never say otherwise. The
streak-at-risk nudge is **suppressed** when a rest day is available: a person
with a freeze in hand does not need to be scared into the app at 19:00.

### Badges

*Duolingo:* dozens, tiered, with monthly challenge badges.

*North:* a **small fixed set**, each tied to something the domain already
records, each earned once, none tiered. First cut, no more than twelve:

| Badge | Earned on |
|---|---|
| First week | 7-day check-in streak |
| First month | 30-day check-in streak |
| Hundred | 100-day check-in streak |
| On time | a goal achieved on or before its target date |
| Checkpoint | first milestone completed |
| Under the bar | first form check completed |
| Consistent | a habit kept 4 weeks without a miss on its schedule |
| Connected | Strava or Apple Health delivering for 30 days |
| Reviewed | 8 weekly reviews read |
| Honest | 20 `helpful` ratings given — *earned for answering, not for the answer* |

The last one is the only place the `helpful` column touches the economy, and
it pays the same for yes and no, which is the whole point of it.

Badges render on a "progress" page under insights and nowhere else. No badge
appears in a nudge, a push, or the coach's replies unless the person brings
it up.

### Leagues, friends, leaderboards — deferred, with reasons

*Duolingo:* weekly leagues of 30 strangers, promotion and demotion, the single
strongest retention lever they have published.

*North:* not built, for three reasons that will still be true when the rest
of this document is built:

- **Single-player by design.** Nothing in the product is visible to another
  account, and the memory the coach holds is the most private data most
  people will ever type into software. A leaderboard is the first surface
  where one person's data reaches another's screen.
- **Cohort size.** A league needs tens of active accounts per bracket per
  week. Below that it is a room of one, which is worse than no room.
- **What it would reward.** XP volume, which under the table above means
  training and habits — good — but also means the person with three habits
  outranks the person with one, which says nothing about either of them.

The door left open: a **self-league** — this week vs your last four — is a
leaderboard with one name on it and no privacy cost. If the numbers say
people want a ladder, build that one first.

---

## Schema sketch

Proposed, not existing. Names follow `DOMAIN.md`'s log shape.

```sql
-- Append-only. Totals are SUM(amount); never stored.
CREATE TABLE xp_ledger (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       text        NOT NULL,            -- table row above, e.g. 'habit_kept'
    amount     int         NOT NULL,            -- negative on reversal
    ref_id     uuid,                            -- the habit, session, milestone, goal, report
    local_date date        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX xp_ledger_user_date_idx ON xp_ledger (user_id, local_date DESC);
-- One award per (user, kind, ref, day). The cap column of the table above
-- is enforced here for the once-per-day kinds and in Go for the "two per day" one.
CREATE UNIQUE INDEX xp_ledger_once_idx ON xp_ledger (user_id, kind, ref_id, local_date)
    WHERE amount > 0;

CREATE TABLE user_badges (
    user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    badge      text        NOT NULL,            -- fixed set in Go, not a table
    earned_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, badge)
);

-- Rest days are a log too: one row per day one was spent.
CREATE TABLE streak_rests (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_date date NOT NULL,
    PRIMARY KEY (user_id, local_date)
);

ALTER TABLE notification_preferences
    ADD COLUMN daily_goal_kind text NOT NULL DEFAULT 'checkin',
    ADD COLUMN daily_goal_xp   int  NOT NULL DEFAULT 15;
```

Badges are a Go constant set, not a table, for the same reason nudge kinds
are strings: adding one should be a line of Go, not a migration. Levels are a
Go table of thresholds. Neither has a row anywhere.

---

## Where it plugs in

`internal/moments` is the seam. Today it answers "is this worth a card". Then
it also answers "is this worth a ledger entry", and the two handlers that call
it already have the funnel wired:

| Hook | Today | Then |
|---|---|---|
| `internal/checkins/handler.go` `momentFor` | `moments.ForStreak` | plus `ledger.Award(streak_day)` when the streak advanced |
| `internal/goals/handler.go` `momentFrom` | `ForGoalAchieved`, `ForMilestoneCompleted` | plus the goal and milestone awards; reversals in `setStatus` when leaving achieved |
| `internal/habits/service.go` `Complete` / `Uncomplete` | nothing | award / reverse `habit_kept` when today is a scheduled day |
| `internal/fitness/strava/sweep.go`, `internal/activity` | nothing | `session_logged` on a new session ≥ 10 min |
| `internal/media` `WithOnReady` | raises `form_ready` nudge | plus `form_check` award |
| `internal/reports` open | nothing | `review_read` within 48h |
| `internal/nudges/service.go` `evalStreakAtRisk` | fires at 18:00 | suppressed while a rest day is held |
| `internal/checkins/service.go` `StreakAt` | consecutive days, 1-day grace | plus rest days from `streak_rests` |
| `internal/coach` context builder | streak via `get_check_in_streak` | plus level title and this week's XP, one line |
| `internal/ai/prompts/weekly_review.md` | "quote their wins" | plus the week's XP and any badge earned, and *only* if the person has the feature on |

A new slice, `internal/ledger`, owns `xp_ledger`, `user_badges`,
`streak_rests`, the award/reverse API, the level table, and the badge rules.
It is called by the slices above and imports none of them; it knows kinds and
amounts, not habits or goals.

---

## Surfaces

- **Dashboard**: the daily ring above the KPI strip; level title under the
  mascot; nothing else. The dashboard is already full.
- **Progress page** (`/app/insights/progress`, exists as a tab): this week's
  XP by kind, the level and what is next, the badge grid, the rest days held.
  This is the only place badges render.
- **Settings → Notifications**: the daily goal picker, and the master switch
  (below).
- **Telegram**: `/streak` and `/week` replies, text only.
- **Mascot**: `celebrate` on a level or a badge, through the same
  `web/shared/moment` card that streaks and goals use today.
- **Nudges**: none. No "you are 5 XP from your daily goal". The streak-at-risk
  nudge is the only loss-framed message the product sends, and it stays about
  the check-in.

---

## Events

Same funnel, same `capture` path in `internal/analytics/funnel.go`:

| Event | Properties | Question it answers |
|---|---|---|
| `xp_earned` | `kind`, `amount` | Which verified actions the economy actually pays for in practice |
| `level_reached` | `level` | Time-to-level as a retention proxy |
| `badge_earned` | `badge` | Whether any badge is reached by more than a handful of people |
| `rest_day_used` | — | Whether freezes save streaks or merely delay the end |
| `daily_goal_met` | `kind` | Whether the ring closes, and how often |

The read: week-4 and week-12 retention, economy-on vs economy-off cohorts.
If the gap is inside noise after eight weeks, the switch stays off and this
document gets a "superseded" note like `docs/byok-plan.md` has.

---

## Rollout

- **Per-account switch**, `users.gamification_enabled`, set by a `main
  gamify <email> on|off` subcommand in the style of `main tier`. Off by
  default. The ledger writes regardless; only rendering is gated, so the
  cohort comparison has the same data on both sides.
- **Cohort by signup week**: alternate weeks on and off for eight weeks, then
  read the events above.
- **Kill switch**: `GAMIFICATION_ENABLED=false` in the environment hides every
  surface for everyone and keeps writing the ledger, so turning it back on
  loses nothing.
- **No retroactive awards.** The ledger starts the day it ships. Backfilling
  three months of habits as XP would hand long-time users a level on day one
  and tell them nothing.

---

## Anti-patterns to refuse

Each of these was a design pressure at some point while writing this, and
each is a way the mechanics above become the thing they were built not to be.

- Buying anything: freezes, boosts, badges, cosmetics.
- XP for opening the app, reading a nudge, sending a message, or rating.
- Loss-framed copy after 21:00 local, or on a rest day, or in a push.
- A badge in a nudge. A badge in the coach's mouth unprompted.
- Merging the check-in streak into the daily-goal streak.
- Storing a total or a level.
- Retroactive awards.
- A leaderboard with another person's name on it.
- Any mechanic that makes a missed rest day, a paused goal, or an abandoned
  goal read as a failure. The product exists for month eight; people who get
  there have paused and abandoned things.
