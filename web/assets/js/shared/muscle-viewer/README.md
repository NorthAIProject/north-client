# Muscle viewer

Files:

- **`muscles.js`** — the muscle taxonomy. `MUSCLE_ALIASES` maps each key to the
  exact `body.glb` mesh names it covers; `MUSCLE_INFO` gives it a display name
  and one-line description for the click-to-inspect panel. Imported by both
  `viewer.js` (in the browser) and `tools/model/build-body.mjs` (in Node, at
  asset-build time), which is why it may never import three.js.
- **`viewer.js`** — the renderer. Scene, materials, glow shader, interaction.
- **`alpine.js`** — the Alpine wrapper used by the in-app component.

Two files make up the muscle taxonomy and must stay in sync — nothing enforces
this automatically, so check both whenever a muscle key changes:

1. **`muscles.js`** (above).
2. **`internal/workouts/plan/muscle.go`** — `MuscleGroups`, the Go-side copy
   of the same 15 keys. This is what constrains `PlanSchema()`'s
   `primary_muscles`/`secondary_muscles`/`stabilizer_muscles` fields via
   `ai.Enum`, so the AI plan generator can only ever return a key that exists
   here.

There is deliberately no third source of truth (no database table, no
exercise catalog): the AI produces muscle assignments directly, per exercise,
constrained to this list. A key present in one file but not the other either
highlights nothing (`viewer.js`'s `setLoads` silently skips unresolved keys)
or can never be produced by the model — both fail quietly, not loudly, so
this checklist is the only thing keeping them aligned.

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

## How the figure is drawn (NOR-6)

The body is **opaque**. Muscles live inside it and are invisible until an
exercise works them, at which point they surface as an ember glow.

That glow can't be conventional lighting — nothing can see a mesh sealed inside
a solid body. Instead each worked muscle is drawn after the skin with
`depthFunc: GreaterDepth`, so it renders *only where the skin is already in
front of it*. The light reads as coming from under the surface. Muscles at zero
load are `visible = false`, which also drops them from the draw list and from
the click-to-inspect raycast.

Two details in `createGlowMaterial()`'s shader are there to stop it looking
wrong, and both are easy to break by "simplifying":

- **Alpha blending, not additive.** Additive is the obvious choice for a glow
  and it fails on the landing page: that card is white and the skin is light,
  so adding light does nothing except where the body is already dark, and the
  figure ends up looking like it is on fire along its silhouette.
- **A depth fade past the figure's centre.** `GreaterDepth` also passes for
  muscles on the *far* side of the body, since nothing but the skin writes
  depth. Without the fade, the torso lights up from behind.

`?muscleDebug=1` renders the skin at 25% opacity, for checking that the two
source meshes in `body.glb` actually fit each other, and lists any mesh in the
asset that no muscle key claims. See `tools/model/README.md`.

## Placeholder arms

`addPlaceholderArms()` is temporary. The shipped `body.glb` has no arms on its
outer body — they were cropped off to dodge a T-pose clash — which leaves hollow
shoulders and arm muscles that can never glow. It stands a capsule in for each
arm until the real body arrives, and disables itself as soon as the skin has
textures. **Delete it when `body.glb` is rebuilt.**

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
