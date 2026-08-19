# NOR-51 — North mascot as an interactive Three.js companion

Linear: https://linear.app/northos/issue/NOR-51
Branch: `fernandocorreia316/nor-51-mascot-companion`

This is the agreed design. Nothing below is implemented yet; the branch exists so
implementation can start from a settled plan.

## Context

North has an approved 2D mascot (`web/assets/brand/north-mascot.png`): a soft-cube head
with one chamfered corner, two large glossy eyes, a flat mouth line, the North eight-point
star glyph on the forehead, a small torso, four stubby limbs — teal/cyan with emissive
speckles. Today it exists in exactly one place: a flat `THREE.Sprite` billboard in the
landing scroll-world (`web/assets/js/landing/scroll-world/panel.js:137`).

NOR-51 asks for a real 3D version living *inside the app*: idle breathing plus reactive
states (`thinking`, `listening`, `celebrate`, `nod`) driven from the coach stream and from
Alpine, mounted on onboarding, the empty dashboard, chat, and settings. Presentation only —
the server stays authoritative, and the whole feature is progressive enhancement with a
static-PNG fallback.

Two decisions were taken before planning.

**1. No Blender: the model is procedural.** There is no 3D artist in this loop, so the
mascot is built from core Three.js geometry rather than shipped as a GLB. That is ~10KB of
JS instead of a 200–500KB asset, and it removes `GLTFLoader`, `AnimationMixer`, the
`tools/model` build step, and any new service-worker cache weight. This deviates from the
ticket's step 2 on purpose. Every acceptance criterion still holds, and the seam stays
clean: a real GLB can later replace `buildMascot()` alone, touching no templ file and no
part of the state API.

**2. Full v1 scope** — ticket steps 3–8, all placements, coach stream wiring included.

Product note carried from the ticket itself: this is labelled *"Polish / delight, not path
to strangers-ready OS."* It was planned with that understood.

## Approach

Copy the muscle viewer, which is already the house pattern for "a templ component that owns
a WebGL canvas".

| Concern | Muscle viewer (existing) | Mascot (new) |
| --- | --- | --- |
| templ component | `web/shared/muscleviewer/muscleviewer.templ` | `web/shared/mascot/mascot.templ` |
| Alpine glue + lifecycle | `web/assets/js/shared/muscle-viewer/alpine.js` | `web/assets/js/shared/mascot/alpine.js` |
| Renderer + states | `.../muscle-viewer/viewer.js` | `.../mascot/scene.js` |
| Geometry | `web/assets/models/body.glb` | `.../mascot/model.js` (procedural) |
| `Script()` OnceHandle | `muscleviewer.templ:158-167` | same |

### 1. Move the star geometry to `shared/` (do this first)

`createStarGeometry()` in `web/assets/js/landing/scroll-world/star.js:37` already rebuilds
the North mark as exact geometry, from four constants measured off `north-logo-mark.png` and
kept in step with `north-logo-mark.svg`. The mascot's forehead glyph *is* that mark, so it
must not be re-measured.

- Move to `web/assets/js/shared/north-star-geometry.js`, contents unchanged.
- Fix the importers under `web/assets/js/landing/scroll-world/` —
  `grep -rl createStarGeometry web/assets/js`.
- Update the file's "if the brand mark is ever redrawn, this is the file that has to follow
  it" header to name both consumers, and the pointer in `web/assets/brand/README.md`.

Required, not optional: a module under `shared/` must not import from `landing/`.

### 2. `web/assets/js/shared/mascot/model.js`

Exports `buildMascot()` → `{ root, parts, dispose }`: a `THREE.Group` plus named handles
(`head`, `torso`, `eyeL`, `eyeR`, `armL`, `armR`, `legL`, `legR`, `glyph`, `speckles`), so
the animator addresses parts by name and never by index.

Core three only — no new vendor file:

- **head** — `ExtrudeGeometry` over a rounded-square `THREE.Shape` with one corner cut flat
  (the chamfer in the PNG), `bevelEnabled: true, bevelSegments: 4` for soft edges on all
  three axes. `RoundedBoxGeometry` is **not** in core three (verified: 0 hits in
  `three.module.min.js`), and vendoring it would inherit the cache-immutability hazard
  documented in `web/assets/js/vendor/README.md`. The extrude route adds no dependency and
  reuses the same technique as the star.
- **torso / 4 limbs** — `CapsuleGeometry`. **eyes** — `SphereGeometry` plus a small second
  sphere as the specular glint (the PNG has two). **mouth** — a thin flattened box.
  **glyph** — `createStarGeometry()` from step 1, emissive, parented to the head.
- **speckles** — one `THREE.Points` cloud (~40 verts sampled on the body surface),
  additive-blended, for the emissive dots. One draw call, no texture.
- Materials: two `MeshStandardMaterial` (body, dark eye) plus one emissive shared by
  glyph and speckles. Colours come from design tokens via `readCSSColor()`
  (`web/assets/js/shared/css-color.js:31`), the way `scroll-world/palette.js:8` does. No hex
  literals.
- **No `theme-changed` subscription.** `/app/*` and the auth shell are hard-coded dark
  (`web/shared/layout/base.templ:10`), which is why `muscle-viewer/alpine.js:59` already
  passes `dark: true`. Tokens are read once at mount, with a comment saying so.
- Every geometry and material goes on a list returned as `dispose()`.

### 3. `web/assets/js/shared/mascot/scene.js`

- Perspective camera framing the mascot, three lights (key, fill, cyan rim), `alpha: true`
  renderer so it sits on the page background. **No `RoomEnvironment`** — it is vendored, but
  a PMREM render per mount is disproportionate for a ~2k-triangle toy.
- `setPixelRatio(Math.min(devicePixelRatio, 1.5))`, the cap the scroll-world README already
  commits to.
- **States are pose targets, not clips.** Each of `idle | thinking | listening | celebrate |
  nod` is a table of per-part target offsets (position / rotation / scale / emissive
  intensity). A critically-damped spring drives current → target every frame, so states
  blend and interrupt cleanly with no `AnimationMixer`. On top of the pose, a per-state
  function of `time` supplies the life:
  - `idle` — torso breathe plus whole-body float, ~0.25Hz
  - `thinking` — head tilt, eyes half-lidded via eye scale-Y, glyph emissive pulse
  - `listening` — head up and forward, glyph steady bright, motion damped
  - `celebrate` — two-bounce hop, arms up, brief emissive burst
  - `nod` — one damped head nod
- `celebrate` and `nod` are one-shots: each records the sustained state it interrupted and
  restores it on completion.
- `prefers-reduced-motion` — no rAF loop at all. Render one frame per state change
  (render-on-demand), the choice `scroll-world/world.js:298` already makes.

### 4. `web/assets/js/shared/mascot/alpine.js`

Mirrors `web/assets/js/shared/muscle-viewer/alpine.js:47-98`:

- Registers `northMascot({ state, id })` before `alpine:init`, loaded as a **non-deferred**
  classic script from a `Script()` OnceHandle — the ordering reason is in that file's doc
  comment at `:14-19`.
- `IntersectionObserver` with `rootMargin: 300px` → dynamic `import()` of `scene.js` (and
  therefore three) only when the component nears the viewport; stop the loop and `dispose()`
  when it leaves. Three never enters the app shell.
- WebGL probe fails → set `failed`; Alpine shows the static `<img>` and the module is never
  fetched.
- Reduced motion is read the same way the other three call sites do
  (`matchMedia("(prefers-reduced-motion: reduce)")`) and passed down as a `reduced` option.
- A local `assetURL()` cache-bust copy, as in `muscle-viewer/alpine.js:27-31` — it has to
  read `document.currentScript.src`, so it cannot be an imported helper.

Public API, module-scoped so it survives htmx swaps:

- `window.NorthMascot.setState(name)` reaches every mounted instance;
  `setState(name, { id })` targets one.
- A `north:mascot-state` CustomEvent on `document` does the same, so server-rendered HTMX
  responses can drive the mascot with no inline JS.
- The module remembers the last **sustained** state and the timestamp of the last
  **one-shot**. A newly mounted instance adopts the sustained state, and replays a one-shot
  only if it was requested within the last second.

That last rule is what makes the chat wiring work. The `sse:done` trigger at
`chat.templ:299`, `:499`, and `:624` re-GETs the page and swaps `#chat-root` **outerHTML**,
destroying any mascot inside it at exactly the moment `nod` fires. The JS module is not
reloaded by that swap, so the remounted instance picks the nod back up.

### 5. `web/shared/mascot/mascot.templ`

`Props{ ID, State, Size, Class }`, with `Size` an enum (`SizeSm|SizeMd|SizeLg`) mapping to
Tailwind box classes. The body is `x-data="northMascot({...})"` — serialized by an
`alpineData()` helper, as `muscleviewer.templ:46-56` does — wrapping a hand-rolled
`<canvas x-ref="canvas" aria-hidden="true">`, carrying the same "no component exists for a
WebGL surface" comment as `muscleviewer.templ:105`. An `x-show="failed"` fallback renders
`<img src="/assets/brand/north-mascot.png" alt="North">`. `Script()` is guarded by
`templ.OnceHandle`, per CLAUDE.md.

### 6. Coach stream wiring

No app-authored generation events exist — chat SSE is fully declarative htmx
(`chat.templ:289-306`, `:473-506`, `:614-631`), and the server emits only `token`, `error`,
and `done` from `internal/coach/handler.go:407`. So the bridge lives in `mascot/alpine.js`
and listens for the htmx events that already bubble to `document`:

| htmx event (`web/assets/js/vendor/htmx-ext-sse.js`) | mascot state |
| --- | --- |
| `htmx:sseOpen` (`:220`) | `thinking` |
| `htmx:sseClose` (`:255-262`, fired by `sse-close="done"`) | `nod`, then back to `idle` |
| `htmx:sseError` (`:201`) | `idle` |

No change to the stream protocol, no Go handler change, no new server events. Guarded so it
only reacts when at least one mascot is mounted.

The mascot that reacts to the coach goes in `chatHeader` (the `h-12` header, `SizeSm`),
which is present for the whole turn — not in the message list that gets swapped. The
empty-state mascots are decorative and sit at rest.

### 7. Placements

| Surface | File | Size | Initial |
| --- | --- | --- | --- |
| Chat header (the reactive one) | `web/chat/chat.templ` `chatHeader` | `SizeSm` | `idle` |
| Chat thread with no messages | `web/chat/chat.templ:270-271` (`len(messages) == 0`) | `SizeMd` | `listening` |
| Chat, no conversations at all | `web/chat/chat.templ:108-134` (`templ Empty`) | `SizeLg` | `idle` |
| Dashboard next-step empty card | `web/app/dashboard.templ:297-309` (`ui.Empty`) | `SizeMd` | `idle` |
| Onboarding form | `web/onboarding/onboarding.templ:79` `FormPage` header | `SizeMd` | `listening` |
| Onboarding done | `web/onboarding/onboarding.templ:210` `DonePage` | `SizeLg` | `celebrate` |
| Settings "About North" | `web/settings/settings.templ:124-150` — **new card, appended last** | `SizeSm` | `idle` |

Settings has no about section today, so that card is the only net-new surface. The ticket
lists it as optional, and it is the natural home for the mascot at rest. Each page adds
`@mascot.Script()` at the end of its body, as `web/workouts/workouts.templ:226` does for the
viewer.

## Files

**New**

- `web/shared/mascot/mascot.templ`
- `web/assets/js/shared/mascot/{model,scene,alpine}.js`
- `web/assets/js/shared/mascot/README.md` — the performance and design contract, in the
  style of `web/assets/js/landing/scroll-world/README.md`

**Moved**

- `web/assets/js/landing/scroll-world/star.js` → `web/assets/js/shared/north-star-geometry.js`

**Edited**

- `web/assets/js/landing/scroll-world/*` — the `createStarGeometry` import path
- `web/assets/brand/README.md` — star-geometry pointer; the mascot row gains its 3D consumer
- `web/chat/chat.templ`, `web/app/dashboard.templ`, `web/onboarding/onboarding.templ`,
  `web/settings/settings.templ` — mounts and `Script()`

**Untouched on purpose**

- `web/pwa/sw.js` — nothing new to precache; `/assets/**` already gets
  stale-while-revalidate, and there is no GLB
- `web/assets/assets.go` — no new top-level asset directory; `js` is already embedded
- `internal/**` — no server change at all

## Tests

- `web/shared/mascot/mascot_test.go`, in the style of `web/shared/layout/base_test.go`: the
  canvas renders; `x-data` carries the initial state and ID; the fallback `<img>` points at
  the brand PNG and has a real `alt`; `Script()` emits exactly once when called twice; each
  size enum maps to its expected classes.
- Extend the existing render tests on the touched pages (`web/chat/chat_test.go`,
  `web/app/dashboard_test.go`, `web/settings/profile_test.go`) to assert the mascot mounts
  and the script tag appears exactly once.
- No JS unit tests — the repo has no JS test runner, and adding one is out of scope.

## Verification

1. `task assets && go tool templ generate && go build ./... && go vet ./...`
   `output.css` is gitignored, so the CSS build has to run before any build or test.
2. `go test ./web/... ./internal/...` — DB tests need `TEST_DATABASE_URL` pointing at the
   docker-compose Postgres on port **5434**, not local 5432.
3. Run the app, then in the browser:
   - every surface in the placement table renders and idles
   - console: `NorthMascot.setState("thinking"|"listening"|"celebrate"|"nod")` — the
     one-shots return to the prior sustained state
   - send a real chat message: the mascot goes `thinking` on stream open, and the `nod`
     survives the `#chat-root` swap on done
   - Network panel: `three.module.min.js` is requested only on mascot pages, and only once
     the component nears the viewport
   - scroll a mascot far offscreen — rAF stops (Performance panel); scroll back — it resumes
   - emulate `prefers-reduced-motion: reduce` — static pose, no rAF loop
   - disable WebGL — static PNG, no console error
   - 375px wide — DPR capped, mascot legible, no layout shift
4. Landing regression: the scroll-world stars still render after the `star.js` move.
