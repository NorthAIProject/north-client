# The calendar terrain

The activity page's scene. Seven columns across is a week, each row back is a
week earlier, and the height of a column is how hard that day was.

## What it is saying

The version this replaced laid activities on a `ceil(sqrt(n))` grid. Both
ground axes were arrangement — they meant nothing — and only height carried
information. Height was elevation gain, which is zero for most of a real
training week, so on a normal account the majority of the field sat flat as
identical rings. It was decoration.

Here every axis is load-bearing:

- **X** is the day of the week, Monday on the left.
- **Z** is the week, receding into the past. Travelling is moving along it.
- **Y** is the day's training load in **MET-minutes** — the MET value of each
  sport multiplied by its moving minutes, summed over the day.

MET-minutes is what lets a gym session and a run stand next to each other. It
is not a complete measure of training stress: it knows nothing about intensity
distribution or heart rate, and 30 minutes of heavy lifting and 30 minutes of
elliptical at the same MET are not the same stimulus. So the interface says
"MET-minutes" and not "training load", and the wire field is `load_met_min`
rather than something generic — when heart-rate streams arrive, TRIMP can sit
beside it instead of quietly replacing it.

**Rest days are drawn**, as flat plates. A week with holes in it is a lie about
the week, and the flat ground is what makes the tall columns mean anything.

## The height curve

Not linear, and the reason is in `geo.js`:

```
height = (1 - exp(-load / scale)) / (1 - exp(-1)) * 0.72 * MAX_HEIGHT
```

`scale` is the 90th percentile of active days over the last 26 weeks, floored,
and computed **once** on the first page. Two things follow from that, both
deliberate:

- **Percentile, not maximum.** One six-hour hike is an order of magnitude above
  a normal session. Scaling to the largest day turns every other day into a
  pancake, which is the exact failure the elevation version already had. There
  is a Go test that fails at max-scaling and passes at p90.
- **Pinned, not per page.** A scale recomputed as pages arrive would silently
  rescale the landscape, so two columns of equal height would mean different
  loads.

Above the scale the curve compresses rather than continuing, and `clipped()`
reports which days ran off the top so the scene can draw them as clipped. An
undocumented non-linear height axis is a lie told in 3D; this is the
documentation.

## Colour

A day takes the colour of the sport family holding the largest share of its
load — but only if that family clears **60%**. Below that it is drawn
desaturated toward the page colour and marked `mixed`, and the tooltip lists
the split. A day that is 51% run and 49% lifting is not a run.

Colours come from `--north-sport-*` in `web/assets/css/input.css`, read through
`readCSSColor`. No hex literals except the pre-stylesheet fallbacks in
`palette.js`. `web/fitness/palette_test.go` fails if the tokens, the fallbacks
and the legend fall out of step.

Which sport belongs to which family is **Go's** answer, on the wire as
`family`. It used to be a set of substring tests in JavaScript, which is how
`Treadmill` came to be neither a run nor anything else.

## Files

| File | Owns |
|---|---|
| `alpine.js` | State, paging, the tooltip, selection. The only file the page loads directly. |
| `scene.js` | Renderer, lights, shared resources, residency, the frame loop. |
| `terrain.js` | A week as GPU objects: one `InstancedMesh`, seven instances. |
| `routes.js` | The line on a day's cap. **The seam for elevation ribbons.** |
| `camera.js` | Clamped drag-orbit and travel along the week axis. |
| `labels.js` | Weekday and week labels, as DOM projected from world space. |
| `palette.js` | Design tokens to `THREE.Color`. |
| `geo.js` | **Pure maths. No three.js import.** |

`geo.js` importing no three.js is a structural rule, not an accident: it is
what lets `web/assets/js/_tests/terrain-geo.test.js` run the real shipped code
under `node --test` with no renderer, no canvas and no DOM. If it ever needs a
`THREE` type, the type is in the wrong place — convert at the boundary.

## Rules that are acceptance criteria, not aspirations

- **Shared resources are disposed once, in `destroy()`.** The box geometry, the
  column material and the per-family line materials are used by every week.
  Disposing one when a week is evicted leaves every later week black. Weeks
  dispose only what they own.
- **Data outlives geometry.** Every page ever fetched stays in memory as JSON —
  a few kilobytes a week. Only the GPU objects for weeks outside
  `[cameraWeek - 4, cameraWeek + 16]` are dropped. Travelling back toward the
  present is free and re-requests nothing. Verify: travel back 40 weeks and
  forward again; `renderer.info.memory.geometries` must flat-line and the
  network tab must stay silent on the way back.
- **Residency is a window, not a queue.** A FIFO either evicts everything
  behind you or holds all forty weeks.
- **One page request in flight.** Travelling fast must not open six fetches. A
  failure stops asking rather than spinning; the strip's own "Load earlier
  weeks" link still works.
- **The wheel belongs to the page.** The scene is most of the viewport; a
  canvas that calls `preventDefault` on wheel traps the reader above the KPI
  strip and the week list. Travel is by drag.
- **Two gates on the frame loop**, intersection *and* `document.hidden`. A
  full-bleed canvas is almost always intersecting, so the intersection check
  alone never fires and the loop runs forever in a background tab.
- **Selection is an outline mesh**, never a rewrite of a week's colour buffer.
- **Selection is keyed by date**, never by array index: weeks are disposed and
  rebuilt, and an index into a list that changes is a bug waiting for someone
  to scroll.
- **Not built below 640px.** A phone has neither the pixels to read a receding
  landscape nor the patience for the download. The list carries the page there.
- **`prefers-reduced-motion` snaps** between weeks instead of gliding, and the
  loop renders on demand rather than every frame. Fog stays; it is not motion.

## What is not here yet

Per-activity altitude streams. `routes.js` is the seam: today it builds a flat
line from a summary polyline at a constant height. The ribbon version takes the
same projected points with a per-point height and returns a tube — same
projection, same fit, different Y source. `has_streams` already rides on the
wire as `false`, and the client already branches on it, so switching it on is a
server change and not a change to the shape of the data.
