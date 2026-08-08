# Scroll world

One WebGL canvas behind the marketing page, fixed and full-viewport, whose camera
travels a path as the page scrolls.

## What it is saying

The landing page is laid out along a time axis — the bearing rail down the left, the
waypoint labels reading `Day 1` and `Month 8` rather than `01` and `02` — because the
claim North is sold on is that it is still useful in month eight.

This is that axis made literal. One line receding into fog, a North mark standing on it
at each section, and every mark the camera has already passed left lit behind it. By the
features section the line is dense with everything already travelled, and the camera has
swung wide enough to show it. That is the argument, not a decoration of it.

It is decoration in the strict sense, though: the page is complete without it. Nothing
here is focusable, nothing here carries information the copy does not, and every failure
path — no WebGL, lost context, narrow viewport, a texture that 404s — ends with an empty
`div` and a page that looks exactly as it did before.

## Files

| File | Owns |
|---|---|
| `world.js` | Renderer, camera, the frame loop, and the rules about when a frame is allowed |
| `geometry.js` | The path, the line, the waypoint instances, the ticks, the motes |
| `star.js` | The North mark as extruded geometry |
| `panel.js` | The two surfaces that use a shipped image or video asset |
| `palette.js` | Design tokens → `THREE.Color` |

Driven by `../scroll.js`, which owns Lenis and ScrollTrigger and is the single source of
truth for how far down the page we are. Booted from `../landing.js` behind an
`IntersectionObserver`, the same way the muscle viewer is.

## Assets it uses

Everything is already embedded and served from `/assets/`. Nothing new was added.

- **`brand/north-logo-mark.png`** — *not* loaded. The eight-point star is rebuilt as
  geometry in `star.js`, because a waypoint is anywhere from two to eighty units from the
  camera and no single raster survives that range. **If the brand mark is redrawn,
  `star.js` has to follow it** — nothing enforces that automatically.
- **`video/north-hero.mp4`** — a `VideoTexture` bound to the *same* `<video>` element the
  page already renders in `heroFilm()`, found by `id="north-hero-film"`. One decoder, one
  download. Removing that id silently costs the panel its film and leaves the poster.
- **`video/north-hero-poster.webp`** — the panel before the video has a frame, and the
  only thing it ever shows under `prefers-reduced-motion`.
- **`brand/north-mascot.png`** — a billboard sprite, loaded on demand once the camera is
  within 0.2 of it. 416KB, never on first paint.

### Not used, and why

**`models/body.glb`** was considered as a distant figure on the line. It is not here:
the muscle demo already renders that exact model, full-size and interactive, in the
section the world dims down for. A second, worse copy of the page's best object,
competing with the real one for the same scroll range, is worth less than the empty space.

**`brand/north-wordmark.png` / `north-logo-wordmark.png`** are dark ink authored for light
backgrounds (`brand/README.md`); the world is dark, and a floating wordmark reads as a
splash screen.

## Performance rules

These are the acceptance criteria, not aspirations:

- Not built at all below 768px wide.
- `setPixelRatio` capped at 1.5.
- No frame while `document.hidden`.
- **While the muscle demo is on screen** the loop drops to ~20fps and the canvas fades to
  15%. That demo owns a second WebGL context; two full-rate loops on one page is the
  likeliest source of jank here. It is throttled rather than paused, because a world that
  freezes and then jumps reads worse than one that is quietly slower.
- Under `prefers-reduced-motion` there is no loop. Frames are rendered on demand from
  `update()`, the motes do not drift, the mascot does not bob, and the panel shows the
  poster.

One thing the plan for this feature called for and this does not do: pausing when the
host element is not intersecting. The host is `fixed inset-0`, so it is always
intersecting and the check would never fire. `document.hidden` is the one that matters.
