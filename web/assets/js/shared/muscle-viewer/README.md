# Muscle viewer (NOR-8)

Three files make up the muscle taxonomy the whole feature is built on. They
must stay in sync — nothing enforces this automatically, so check all three
whenever a muscle key changes:

1. **`viewer.js`** — `MUSCLE_ALIASES` maps each key to the exact `body.glb`
   mesh names it colours, and `MUSCLE_INFO` gives it a display name and
   one-line description for the click-to-inspect panel.
2. **`internal/workouts/plan/muscle.go`** — `MuscleGroups`, the Go-side copy
   of the same 15 keys. This is what constrains `PlanSchema()`'s
   `primary_muscles`/`secondary_muscles`/`stabilizer_muscles` fields via
   `ai.Enum`, so the AI plan generator can only ever return a key that exists
   here.

There is deliberately no fourth source of truth (no database table, no
exercise catalog): the AI produces muscle assignments directly, per exercise,
constrained to this list. A key present in one file but not the others either
highlights nothing (`viewer.js`'s `setLoads` silently skips unresolved keys)
or can never be produced by the model — both fail quietly, not loudly, so
this checklist is the only thing keeping them aligned.

## Adding a new muscle key

1. Confirm the target mesh(es) exist in `body.glb` under their Z-Anatomy
   names (see `web/assets/models/README.md`).
2. Add the key + its mesh-name aliases to `MUSCLE_ALIASES` in `viewer.js`.
3. Add a display name + description to `MUSCLE_INFO` in the same file.
4. Add the key to `MuscleGroups` in `internal/workouts/plan/muscle.go`.
5. Regenerate no schema/migration is needed — `PlanSchema()` reads
   `MuscleGroups` directly, and plans are stored as `jsonb`.

## Adding a new exercise

Nothing to register. Exercise names are free text written by the AI plan
generator (`internal/workouts/plan.PlanSchema()`); it's asked, per exercise,
to pick 0+ keys from the same canonical list above for
`primary_muscles`/`secondary_muscles`/`stabilizer_muscles`. There is no
exercise-name lookup table to keep updated.

## Two call sites, one data contract

- **Landing page** (`web/landing/demos.templ` + `landing.js`): calls
  `setLoads([{key, share, role}])` directly with its own hand-written
  per-muscle percentages, for a richer marketing-page readout.
- **Production** (`web/shared/muscleviewer.Viewer` + `alpine.js`): calls
  `setMuscleGroups({primary, secondary, stabilizers})` — flat key arrays,
  no percentages, because that's what the AI actually returns. This is a
  thin adapter over the same `setLoads` internals (fixed intensity per tier).

Both consume the same `viewer.js` module and the same `body.glb`.
