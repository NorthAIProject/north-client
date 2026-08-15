# DOMAIN.md

> This document describes North's **domain model**: what the product tracks about a
> person's life, how those concepts differ from each other, and where each one lives
> in the code.
>
> `ARCHITECTURE.md` explains how the system is built. This explains what it is about.
>
> Intended for both human contributors and AI coding agents.

---

# Why this document exists

North began as a training app. For a while, "what does the product know about you"
was answerable by reading `internal/workouts`.

It is not anymore. A Personal Growth OS coaches across the whole of someone's life,
which means a growing number of slices each own a piece of the picture, and the
interesting behaviour — the coach noticing that bad sleep is what actually broke
this week's training — lives in how those pieces combine rather than in any one of
them.

This document is the map of those pieces. Read it before adding a new one.

---

# The three shapes

Nearly everything North tracks is one of three things. Getting this distinction
right is the difference between a coherent model and a pile of tables, because each
shape has a different lifecycle, a different storage pattern, and a different way of
being summarised for the coach.

## 1. Logs — things that happened

A log is a **record of an event, at a point in time**. It is never "due", it cannot
be missed, and it is not right or wrong. It only accumulates.

| Log | Slice | Table |
|---|---|---|
| A tracked bout of exercise | `internal/activity` | `activity_sessions` |
| Something eaten | `internal/meals` | `food_logs` |
| Water drunk | `internal/hydration` | `hydration_logs` |
| A night's sleep | `internal/sleep` | `sleep_logs` |
| A daily reflection | `internal/checkins` | `check_ins` |
| A journal entry | `internal/mind` | `journal_entries` |
| A body measurement | `internal/biometrics` | `user_biometrics` |
| A form-check video analysis | `internal/media` | `form_analyses` |

Logs come in two storage flavours, and which one to use is decided by **how often a
person records it**:

- **Append-per-event** — several entries per day, aggregated on read. Water and
  food work this way: you drink a glass at a time. Pattern: a `log_date` column and
  a `(user_id, log_date DESC)` index. Reference: `migrations/00016_create_food_logs.sql`.
- **Upsert-per-day** — at most one per local day, edited in place. Sleep and
  check-ins work this way: you did not have two nights last night. Pattern:
  `UNIQUE (user_id, local_date)`. Reference: `migrations/00009_create_check_ins.sql`.

Both compute `local_date` in the **user's** timezone at write time, so "today"
means their today rather than UTC's.

## 2. Habits — things that should keep happening

A habit is a **recurring intention with a schedule**. Unlike a log it *can* be
missed, which is the entire point: adherence and streaks are only meaningful
against an expectation.

| Concept | Slice | Table |
|---|---|---|
| A recurring behaviour + its completions | `internal/habits` | `habits`, `habit_completions` |
| A meal reminder | `internal/meals` | `meal_reminders` |

Schedules are stored as `days_of_week smallint[]` matching Go's `time.Weekday`
(0 = Sunday). Per-day idempotency is enforced by a uniqueness constraint —
`UNIQUE (habit_id, local_date)` for completions, `last_fired_local_date` for
reminders.

**Streaks are schedule-aware.** A habit scheduled for Mondays, Wednesdays and
Fridays must not break its streak on a Tuesday. This is the one place in the model
where the obvious implementation is wrong, and it is why `habits` computes streaks
itself rather than reusing the simpler consecutive-days logic in
`internal/checkins`.

## 3. Objectives — things being aimed at

An objective is a **standing target or intention**. It is not an event and has no
schedule; it is the thing logs and habits are measured against.

| Objective | Slice | Table |
|---|---|---|
| A stated goal, in the user's words | `internal/goals` | `goals`, `goal_updates`, `goal_milestones` |
| Calorie and macro targets | `internal/calculator` | `user_macro_plans` |
| Diet preferences (vegan, low-carb…) | `internal/meals` | `user_diet_preferences` |
| Standing settings (units, default goal) | `internal/preferences` | `user_preferences` |
| A generated training plan | `internal/workouts` | `workout_plans` |

## Where imported data fits

A log does not have to be typed by a person. `internal/fitness/strava` imports
recorded activities into the same `activity_sessions` table that in-app tracking
writes to, marked `source='strava'` with the provider's id in `external_id`.

Two rules keep that honest:

- **Imports are logs like any other.** They are not a parallel table, a separate
  history, or a different type. A run is a run whether a watch recorded it or a
  person tapped start.
- **Dedupe belongs in the schema.** `UNIQUE (source, external_id)` plus
  `ON CONFLICT DO NOTHING` is what lets a sync run as often as it likes without
  anyone reasoning about it. A provider integration that had to remember what it
  had already imported would eventually get it wrong.

Provider integrations live under `internal/fitness`, which its package doc
claims for exactly this. They are **not** sign-in: disconnecting a provider must
never affect someone's ability to log in, which is why Strava has its own table
rather than a row in `auth_identities`.

---

# Life domains

Cutting across all three shapes is the question of **which part of a life** something
belongs to. That vocabulary lives in exactly one place:

**`internal/shared/lifedomain`** — `fitness`, `health`, `work`, `learning`,
`personal`, `other`.

It is a plain Go list with no dependencies, following the same pattern as
`memory.Categories` (`internal/memories/memory/memory.go`): one list, consumed by
UI selects, write-side validation, and AI enum schemas alike. Columns holding a
domain are plain `text` with **no CHECK constraint** — validation is Go-side, so
adding a domain is a code change and never a migration.

`goal.Categories` is an alias of this list. Habits use it directly.

Note what this vocabulary is **not**:

- It is **not** the package layout. `internal/meals` and `internal/activity` are
  both "fitness"; `internal/mind` spans health and personal.
- It is **not** the navigation. `layout.BuildNav` groups by where a user expects to
  click (Fitness / Mind / Care), which is a UX question, not a modelling one.
- It is **not** `memory.Categories`, which classifies *kinds of fact*
  (`injury`, `equipment`, `schedule`) rather than areas of life. A memory could
  reasonably carry both one day; they are orthogonal.

Keeping these separate is deliberate. They answer different questions and they
change for different reasons.

---

# Scheduled nudges

Accountability that does not wait for the person to open chat lives in
`internal/nudges`. A worker job evaluates deterministic rules (missed check-in,
approaching goal deadline) in the user's timezone and writes `user_nudges`
rows. The web process only lists, marks read, and dismisses. The coach model
does not produce these notes and they do not go into the context block.

---

# How the coach sees all of this

Everything above is only worth storing because it reaches the coach. That happens
through exactly one interface, in `internal/coach/context_builder.go`:

```go
type ContextSource interface {
    Name() string
    Collect(ctx context.Context, req ContextRequest, into *Context) error
}
```

A slice contributes by writing a `context_source.go` and being registered in the
`coach.NewContextBuilder(...)` varargs in `cmd/web/main.go`. Rules worth knowing
before writing one:

- **Sources hand over prose, never structures.** Each domain type carries a
  `Summary() string`; the coach receives rendered text. This is what keeps the
  prompt readable in a log when someone is working out why the coach said what it
  said.
- **A failing source degrades the reply rather than failing it.** A coach that
  cannot reach the goals table should still answer, having correctly said it cannot
  see the user's goals.
- **Empty sections are labelled, not omitted.** "Hydration: nothing logged yet"
  tells the model there is nothing; silence invites it to invent something.
- **Several sources may share one heading.** `FitnessSummary` is written by both
  `calculator` and `activity`; `DailySignals` by both `sleep` and `hydration`. One
  field per slice would make the prompt a list of near-duplicate headings, which
  reads worse and costs more.

---

# Adding a new domain

The checklist, in order:

1. Decide which of the three shapes it is. If it is a log, decide append-per-event
   or upsert-per-day. Copy the referenced migration.
2. Add a goose migration in `migrations/` (contiguous numbering) and a `db/queries.sql`,
   then register the package in `sqlc.yaml` and regenerate.
3. Build the slice: leaf domain package (`internal/<slice>/<thing>/<thing>.go`) with
   a `Summary()` method, then `repository.go`, `service.go`, `handler.go`. Handlers
   call their own service, never their own repository.
4. Write a `context_source.go` and register it in `cmd/web/main.go`. Prefer an
   existing `Context` heading over inventing one.
5. Wire the UI. A new top-level page needs a `layout.BuildNav` entry; anything
   daily probably belongs on `/app/care` instead.
6. If it needs background work: a `jobs.Kind` const plus payload struct in
   `internal/jobs/jobs.go`, and `worker.Register(...)` in `cmd/worker/main.go`.
   Note there is **no scheduler** — jobs are triggered by events or by a person,
   and anything recurring is polled at request time.
7. Add tests against a real Postgres via `testdb.New(t)`. There are no mocks;
   cross-slice dependencies are faked with small local interfaces.
8. Update this document.

---

# What is deliberately not modelled

- **No generic `Activity` supertype.** Workouts, cardio sessions and meals stay
  distinct types rather than rows in one polymorphic table. The abstraction would
  have to be invented before anything needs it, and North's rule is that structure
  earns its place by being demanded, not predicted.
- **No event-sourcing.** Logs are rows, not events with projections.
- **No per-domain check-in.** `check_ins` stays one row per day covering mood and
  energy overall. Splitting it per domain would ask people to file their feelings.
- **No scheduler.** See step 6 above.

These are current positions, not permanent ones. Each becomes worth revisiting when
something concrete is blocked by its absence — and when that happens, the reason
belongs in this section.
