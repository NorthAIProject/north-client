# The mascot companion

Three files, one component: `web/shared/mascot/mascot.templ` renders a canvas,
`alpine.js` decides when it lives, `scene.js` runs it, `model.js` is the mascot.

## Why there is no GLB

The ticket (NOR-51) assumed a Blender-authored `mascot.glb` under 500KB. There
was no modeller, and the shape — a soft cube, a capsule torso, four capsule
limbs, two spheres — is simple enough that building it in code costs ~10KB and
stays editable by whoever is already in the repo.

Skipping the asset also skips `GLTFLoader`, `AnimationMixer`, the `tools/model`
build step, and any new service-worker cache weight.

`buildMascot()` in `model.js` is the seam. A real GLB later means rewriting that
one function: `scene.js` only ever addresses parts by name, and no templ file
knows the model exists.

The one thing that must not be re-derived is the forehead glyph. It is the North
mark, and it comes from `shared/north-star-geometry.js`, whose constants were
measured off `brand/north-logo-mark.png`. See `brand/README.md`.

## States are poses, not clips

`idle`, `thinking`, `listening` are sustained. `celebrate` and `nod` are
gestures: they play once and hand control back to whatever was showing.

Each state is a table of scalars (head tilt, eyelid, arm raise, glow), and a
critically-damped spring drives current toward target every frame. That is what
lets a state interrupt another mid-motion without popping, and it is why there
is no mixer here — a mixer needs clips, clips need a rig, a rig needs the GLB.

On top of the pose sits a function of time: the breathe, the float, the hop, the
nod. The pose says where the mascot is; the time function is what makes it look
alive.

## Who drives it

```js
window.NorthMascot.setState("thinking")            // every mounted mascot
window.NorthMascot.setState("nod", {id: "chat"})   // one of them
```

or, from a server-rendered response, a `north:mascot-state` CustomEvent on
`document` with `{state, id}` in its detail.

The coach stream drives it automatically. Chat has no app-authored generation
events — the stream is declarative htmx and the server emits only
`token`/`error`/`done` — so `alpine.js` listens to what htmx already bubbles:

| htmx event | state |
| --- | --- |
| `htmx:sseOpen` | `thinking` |
| `htmx:sseClose` | `nod`, then `idle` |
| `htmx:sseError` | `idle` |

**The state lives on the module, not on the component.** In chat,
`sse-close="done"` re-GETs the page and swaps `#chat-root` outerHTML, destroying
the mascot at the exact moment the reply finishes and the nod should play. An
htmx swap does not reload the script, so a mascot mounting into the new DOM
adopts the sustained state and replays a gesture asked for in the last second.

## Performance contract

- three is imported only when a mascot nears the viewport (`IntersectionObserver`,
  `rootMargin: 300px`), and the WebGL context is freed when it leaves. The app
  shell never pays for it.
- `devicePixelRatio` capped at 1.5.
- No frame while `document.hidden`.
- Under `prefers-reduced-motion`: no rAF loop at all. The springs settle
  instantly and one frame is drawn per state change. Gestures are not played —
  they are motion and nothing else.
- No environment map. `RoomEnvironment` is vendored, but a PMREM render per
  mount is disproportionate for a ~2k-triangle model that may appear more than
  once on a page.
- Every failure path ends with the flat `brand/north-mascot.png`, and the module
  is never fetched when WebGL is missing.

## Placement

Not on every page — that is the performance tax the ticket warned about. The
mascot appears where the page is otherwise empty or is explicitly about North:
onboarding, empty chat, the empty dashboard card, settings, and small in the
chat header, which is the one that reacts to the coach.
