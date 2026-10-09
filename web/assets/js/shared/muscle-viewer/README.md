# Muscle viewer

Files:

- **`muscles.js`** — the muscle taxonomy. `MUSCLE_ALIASES` maps each key to the
  atlas mesh names it covers (read by `tools/model/build-body.mjs` and
  `scripts/bodymap`); `MUSCLE_INFO` gives it a display name
  and one-line description for the click-to-inspect panel. Imported by both
  `viewer.js` (in the browser) and `tools/model/build-body.mjs` (in Node, at
  asset-build time), which is why it may never import three.js.
- **`viewer.js`** — the renderer. Scene, region materials, interaction.
- **`alpine.js`** — the Alpine wrapper used by the in-app component.

Two files make up the muscle taxonomy and must stay in sync — nothing enforces
this automatically, so check both whenever a muscle key changes:

1. **`muscles.js`** (above).
2. **`internal/workouts/plan/muscle.go`** — `MuscleGroups`, the Go-side copy
   of the same 17 keys. This is what constrains `PlanSchema()`'s
   `primary_muscles`/`secondary_muscles`/`stabilizer_muscles` fields via
   `ai.Enum`, so the AI plan generator can only ever return a key that exists
   here.

The exercise catalog (`exercises.primary_muscles` / `secondary_muscles`)
stores keys from the same list, and is what logged sets are mapped through:
the plan page's Muscles card lights the body from `lift.LoadOf` — fatigued,
recovering, trained this week — via `setMuscleGroups`' three tiers. A key
present in one file but not the other either highlights nothing
(`viewer.js`'s `setLoads` silently skips unresolved keys) or can never be
produced by the model — both fail quietly, not loudly, so this checklist is
the only thing keeping them aligned.

`build-body.mjs` is the one place that fails loudly: it refuses to write a
`body.glb` that has no geometry for some key in `MUSCLE_ALIASES`.

## Adding a new muscle key

1. Confirm the target mesh(es) exist in `body.glb` under their Z-Anatomy
   names (see `web/assets/models/README.md`).
2. Add the key + its mesh-name aliases to `MUSCLE_ALIASES` in `muscles.js`.
3. Add a display name + description to `MUSCLE_INFO` in the same file.
4. Add the key to `MuscleGroups` in `internal/workouts/plan/muscle.go`.
5. No schema or migration is needed — `PlanSchema()` reads `MuscleGroups`
   directly, and plans are stored as `jsonb`.

## How the figure is drawn

`body-map.glb` is the skin of a body cut into one region per muscle key (see
`web/assets/models/README.md`). Every region has its own material, so colouring is
setting a colour: untrained regions are `--north-body-idle`, skin with no muscle
under it is `--north-body-base`, and heat ramps from `--north-heat-low` to
`--north-ember`. The tokens live in `web/assets/css/input.css` and are mirrored to
iOS.

Every colour is decided on the server. `muscleviewer.templ` turns either an
exercise's tiers or a body map (`internal/bodymap`) into region heat, folds muscles
without skin into the region over them, and hands the viewer a finished
`{region: 0..1}` map plus the localized tap details. Without WebGL the same heat
paints the flat front/back SVG generated beside it.

The figure sways slowly around a front three-quarter view (back three-quarter when
back muscles are the hottest), never under reduced motion; drag turns it.

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

Both consume the same `viewer.js` module and the same `body-map.glb`.
