# The mascot companion

Two files, one component: `web/shared/mascot/mascot.templ` renders the Khepri
scarab PNG, `alpine.js` writes `data-state`, and CSS in `web/assets/css/input.css`
is what actually moves it.

## Why there is no Three.js path

NOR-51 built a procedural cube in WebGL because the only approved still was a
soft-cube head. The live mascot is now Khepri, shipped as
`brand/khepri-mascot.png`. Rebuilding a scarab from primitives would not match
the still, and would keep the WebGL tax on chat, onboarding, dashboard, and
settings. The still is the mascot.

Landing still uses Three.js for the scroll-world. Its companion is the same PNG,
loaded as a `THREE.Sprite` in `scroll-world/panel.js`.

## States are poses, not clips

`idle`, `thinking`, `listening` are sustained. `celebrate` and `nod` are
gestures: they play once (`animationend`) and hand control back to whatever was
showing.

Each state is a CSS animation on the `<img>`. Under `prefers-reduced-motion` the
animations are not applied, and alpine.js skips gestures entirely.

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

- No Three.js, no canvas, no IntersectionObserver. The page pays for a PNG.
- Under `prefers-reduced-motion`: no animation at all. Gestures are not played.
- The image is decorative (`alt=""`, `aria-hidden`) because every surface that
  mounts it already names the product in copy.

## Placement

Not on every page. The mascot appears where the page is otherwise empty or is
explicitly about the coach: onboarding, empty chat, the empty dashboard card,
settings, and small in the chat header, which is the one that reacts to the
coach.
