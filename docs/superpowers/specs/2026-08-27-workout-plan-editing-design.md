# Editing a training plan

Design doc. 2026-08-27.

## Context

North generates a training plan and then never lets anyone touch it. The intake
form asks six questions, the model returns a plan, and `/app/training/{id}`
renders it read-only. There is no route that mutates a plan — the only way to
change one exercise is to answer the intake again and regenerate the whole
thing.

That is the wrong shape for the product. A plan is followed for weeks; the gym
is busy, a shoulder aches, a lift turns out to be wrong for someone. Wanting to
swap one movement is the normal case, not an edge case.

The goal is **AI drafts, the person edits** — not a from-scratch manual builder.
The generated plan stays the starting point, because a blank weekly grid is a
worse product for almost everyone, and because building from a generated plan
sidesteps the schema constraints described below.

## What already exists and helps

- `plan.Exercise.CatalogSlug` is already the join between a plan entry and a
  catalog row. The model is asked to echo a slug it was shown, so plan
  exercises already reference the catalog rather than being free text.
- `Service.Candidates` (`internal/workouts/service.go:310`) already returns
  catalog rows filtered to the equipment someone owns — the same list a human
  picker needs.
- The catalog browse UI is filterable by muscle, category and equipment, and
  paged, as of `migrations/20260827150000`.
- `plan.Validate(plan, intake) []string` already reports what breaks an intake's
  constraints, and returns problems rather than a bare error.
- Plan exercises now carry `IllustrationSlug`, so any picker can show the
  movement rather than only naming it.

## Constraints found in the schema

`migrations/00004_create_workouts.sql`, and all of these matter:

- `workout_plans.intake_id` is **NOT NULL** with an FK to `workout_intakes`. A
  plan with no intake cannot be stored.
- `workout_plans.model` and `provider` are **NOT NULL**. A hand-built plan has
  no generation to name.
- `plan` is a single `jsonb` column holding the whole plan body, so any
  per-exercise change is a read-modify-write of the entire plan.
- There is **no UPDATE query for plans**. `internal/workouts/db/queries.sql`
  inserts and selects; the table is append-only in practice, and `ListPlans`
  orders by `created_at DESC` while `/app/training` redirects to the newest.

The last point is the useful one: the table already behaves like a history.

## Design

### An edit inserts a new plan row

Every edit writes a new `workout_plans` row rather than mutating one.

This falls out of what is already there. The row carries the original's
`intake_id`, `model` and `provider`, so both NOT NULL columns stay satisfiable
without a schema change and without inventing a fake intake. `/app/training`
already shows the newest row, so the edited plan becomes the live one with no
routing change. And nothing is destroyed — CLAUDE.md is explicit that historical
information should be summarized rather than discarded, and an edit that
silently overwrites the model's original is exactly that kind of loss.

One migration, purely for provenance:

```sql
ALTER TABLE workout_plans
    ADD COLUMN source      text NOT NULL DEFAULT 'ai',
    ADD COLUMN edited_from uuid NULL REFERENCES workout_plans (id) ON DELETE SET NULL;
```

`source` is `'ai'` or `'edited'`. Unconstrained text, matching how `category` is
handled on `exercises` — a CHECK here would need migrating the first time a
third provenance appears.

`ON DELETE SET NULL` rather than CASCADE: losing an ancestor must not delete the
plan someone is currently following.

Carrying `model` and `provider` forward is deliberate. They record which
generation this plan descends from, which stays true after an edit; `source`
is what says a human touched it. The pair is more informative than blanking
either.

### Edit operations are pure functions

In `internal/workouts/plan/edit.go`, beside `Validate` and `NextSession`:

```go
func Swap(p Plan, day, index int, with Exercise) (Plan, error)
func Insert(p Plan, day, index int, ex Exercise) (Plan, error)
func Remove(p Plan, day, index int) (Plan, error)
func Move(p Plan, day, from, to int) (Plan, error)
func SetPrescription(p Plan, day, index int, sets int, reps string, rest int) (Plan, error)
```

Each takes a plan and returns a new one, touching no database and no context.
Out-of-range day or index is an error rather than a panic, because the indices
arrive from a URL.

Pure functions because the interesting behaviour — what a swap preserves, what a
move does to the rest of the day — is worth testing directly, and because the
plan package is already where plan semantics live.

**Swap keeps the prescription and replaces the movement.** Sets, reps, rest and
substitute stay; name, equipment, catalog slug, illustration and muscles come
from the new catalog row. Someone swapping a barbell row for a dumbbell row
wants the same work, not a reset.

### The service wraps them

`internal/workouts/service.go` gains one unexported helper and a thin method per
operation:

```go
func (s *Service) applyEdit(ctx context.Context, user users.User, planID uuid.UUID,
    edit func(Plan) (Plan, error)) (StoredPlan, error)
```

It loads the plan for that user, applies the edit, validates, and inserts the
new row. One path means one place for authorization and one place where the
new-version rule lives. The public methods (`SwapExercise`, `AddExercise`,
`RemoveExercise`, `MoveExercise`, `SetPrescription`) exist as named methods
rather than an exported `EditOp` enum, per CLAUDE.md's preference for explicit
over clever.

### Validation warns, it does not block

`plan.Validate` still runs, but for a human edit its problems are surfaced as a
notice on the plan page rather than rejecting the edit.

The intake is a description of a typical week, not a contract. Someone who said
"dumbbells" may be in a hotel gym with a barbell today, and refusing their edit
because a form said otherwise is the software being wrong more confidently than
the person. The AI path keeps blocking — it already retries on validation
failure, and a model inventing unavailable equipment is a defect rather than a
choice.

### Routes

Mounted under the existing `/training` group in
`internal/workouts/handler.go`. All mutations are POST; all render an HTMX
fragment.

```
GET  /training/{id}/days/{day}/exercises/{index}/swap   the picker panel
POST /training/{id}/days/{day}/exercises/{index}/swap   catalog_slug
POST /training/{id}/days/{day}/exercises/{index}/remove
POST /training/{id}/days/{day}/exercises/{index}/move    direction=up|down
POST /training/{id}/days/{day}/exercises/{index}/sets    sets, reps, rest
POST /training/{id}/days/{day}/exercises                 catalog_slug (add)
```

**The URL goes stale, and that has to be handled.** Each edit produces a new
plan id, so the page rendered at `/app/training/{old}` is now showing a
superseded plan. The response swaps the affected day card and sets
`HX-Push-Url: /app/training/{new}`, so a reload lands on what is actually on
screen.

**Concurrent edits branch the history.** Two tabs, or a double-submitted button,
would each fork from the same parent and one would silently win. The `{id}` in
the path is the plan the fragment was rendered from; if it is not the newest for
that user, respond `409` and re-render the day from the current plan rather than
writing a fork.

### Reorder without a new dependency

Move up / move down buttons, not drag-and-drop. No sortable library is vendored,
Alpine is the only JS in the tree, and CLAUDE.md's dependency test asks whether
the standard approach solves it first. Two buttons posting to `move` need no
JavaScript at all and work on a phone in a gym, which is where this screen is
actually read. Drag can be added later without changing the endpoint.

### The swap panel, and whether the AI belongs in it

The panel is the catalog picker, filtered by default to the muscles the current
exercise trains and the equipment the intake recorded, with a ranked strip of
suggestions pinned above it. The strip is the same rows, ordered differently.

The picker markup belongs in a shared package rather than being reached for
across slices. `web/exercises` owns the browse page and `web/workouts` owns the
plan page, and a template importing another page package would be the first such
edge in the tree. Extract the row into `web/shared/exercisepicker` and have both
call it — a smaller change than it sounds, since `exerciseart.Frames` already
carries the part that used to be page-specific.

**Not done.** The picker lives in `web/workouts` and has one caller, so the
duplication the extraction would prevent does not exist yet. Move it when the
second caller appears rather than before.

**Start the ranking heuristic, not generated.** The catalog carries
`primary_muscles`, `secondary_muscles` and `equipment` as structured columns, so
"same primary muscle, equipment they have, different slug" is a SQL ORDER BY and
answers most swaps. An LLM call on every panel open costs money and latency on a
screen that should feel instant, and North meters every model call precisely so
that this kind of spend is a decision rather than a habit.

Revisit with an LLM only if the heuristic proves insufficient in use — CLAUDE.md
says to measure first. This is the one place in the design where the cheap
version might simply be the right version.

## Phases

Each is independently shippable and independently useful.

1. **Migration, `Swap`, service, picker.** The highest-value edit and the one
   that carries all the plumbing: new-version storage, the 409 guard, the
   `HX-Push-Url` handling, the picker panel. Only `Swap` is written in this
   phase — the other four pure functions arrive with the phases that use them,
   so nothing ships untested behind an unused code path.
2. **Add and remove.** Add needs one decision the others do not: what
   prescription a newly added exercise starts with. The catalog has no sets or
   reps columns — it describes movements, not dosage — so there is nothing to
   copy and the default is a choice. Proposal: 3 sets, `8-12`, 90s rest, chosen
   to be obviously generic rather than falsely specific, and immediately
   editable via phase 3.
3. **Reorder and prescription editing.** Fiddliest UI, least conceptual risk.
4. **Editing from the coach chat.** A follow-on, not an alternative: once the
   operations exist as service methods, exposing them as an agent capability in
   `internal/agent/capabilities.go` is thin. It has to come after, because it
   needs those primitives to exist.

## Out of scope

- A from-scratch manual plan builder. It needs both NOT NULL columns relaxed and
  an entire second flow, and it is the worse product for most people.
- Drag-and-drop reordering.
- Editing an older *version* of a plan. Opening one by URL and editing it is
  refused: the guard compares against the newest version of that plan, so a
  stale handle gets a 409 and is healed to the current version. Rewriting
  history is a different feature and probably a confusing one.

  Note this is narrower than the spec originally said. It claimed editing any
  plan that was not the account's newest was out of scope, and the guard was
  built that way — comparing against the newest row for the whole account. That
  made every plan but the most recent permanently uneditable, which nobody could
  reach until the plans list linked to them. "Superseded" now means *this plan*
  changed under me; generating a second plan is not a conflict with editing the
  first.

## Verification

- `internal/workouts/plan/edit_test.go` — the operations as pure functions:
  swap preserves the prescription, move at the boundaries, out-of-range indices
  error rather than panic, and no operation mutates its input.
- `internal/workouts/` — an edit inserts a row rather than updating one, the new
  row carries the parent's `intake_id` and points at it via `edited_from`, the
  original is still readable afterwards, and `/app/training` resolves to the
  edited version.
- A stale `{id}` returns 409 and writes nothing.
- `web/workouts/` render tests for the picker panel and the edited day card,
  including that a validation warning renders as a notice and not as a blocked
  edit.
- Manually: swap a lift on a real plan, reload the pushed URL, confirm the plan
  shown is the edited one and the original is still in `ListPlans`.
