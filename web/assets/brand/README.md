# North brand assets

Transparent files for light and dark UI shells. Backgrounds are fully transparent so the
surrounding theme shows through — never place these on a plate.

## Files

| File | Use |
|------|-----|
| `north-logo-mark.svg` | **The mark.** Fixed brand gradient, works on light and dark. What `ui.BrandMark` serves. |
| `north-logo-mark-mono.svg` | Single-colour mark, `fill="currentColor"`. For inlining where the surrounding text colour should drive it. |
| `favicon.svg` | Tab icon. Flat fill, opaque, `prefers-color-scheme` for dark tab strips. |
| `north-logo-mark.png` | Raster fallback, and the source the PWA plates are generated from (`go run ./scripts/pwa-icons`). |
| `pwa-180.png` | iOS apple-touch-icon. Opaque `#1C1C1F` plate. |
| `pwa-192.png` | Manifest `any` 192. Opaque `#1C1C1F` plate. |
| `pwa-512.png` | Manifest `any` 512. Opaque `#1C1C1F` plate. |
| `pwa-512-maskable.png` | Manifest `maskable` 512. Same plate, star inset for Android adaptive crop. |
| `north-wordmark.png` | App name. Dark ink. **Currently unused** — see below. |
| `north-logo-wordmark.png` | Symbol + name lockup. **Currently unused** — see below. |
| `north-mascot.png` | Companion cutout. Billboard in the landing scroll-world (`scroll-world/panel.js`), and the no-WebGL fallback for the 3D companion (`shared/mascot/`). |

Originals with backgrounds live in `source/` for design reference only — never serve them.

## Theme strategy

**The mark carries fixed brand colour on both themes.** Its gradient runs from a light core
through mint to near-black navy, which reads on white and on the dark shell without changing.
Nothing needs to swap it, and nothing should.

**The wordmark is live text, not an asset.** `ui.BrandLockup` renders the mark beside the
literal word *North* in a Tailwind class, so it inherits `text-foreground` and themes for free.
That is why the two wordmark PNGs are referenced nowhere: the product does not need them, and
the ticket that asked for a dark-mode wordmark (NOR-52) was describing a problem the asset has
and the app does not.

**`filter: invert(1)` is not the answer** and this file used to recommend it. Inverting a
brand asset produces a colour nobody approved; it happened to look acceptable for a
near-black wordmark and would not survive the mark's gradient.

## Geometry, and the one thing that must stay in step

The mark is **two shapes**, not one:

```
body     eight even tips at 0.63, waists at 0.25
needle   a thin spike across the 45° axis, out to 1.00 up-right and 0.92 down-left
```

Both were measured off `north-logo-mark.png`'s alpha channel, not estimated. The giveaway is
the ray width against radius — 17° a third of the way out, 10° at half, then 4° and 3° near
the tip. Width collapses long before radius does, which is a needle laid over an even star
rather than a star with one long point. Modelling it as a single polygon gives a blunt
asterisk; pulling the flanks concave to compensate gives a rounded blob. Both were tried.

**`web/assets/js/shared/north-star-geometry.js` builds the 3D star from the same four
numbers.** They had already drifted once — that file modelled a symmetric star with long
cardinal points, which the mark has never been. If the mark is redrawn, both files change or
they diverge again.

Two scenes draw that geometry: the landing scroll-world's waypoints, and the glyph on the
mascot's forehead. Redrawing the mark moves all three surfaces at once.

## Serving

Embedded via `web/assets` (`//go:embed brand …`); new files are picked up with no wiring.
Go's `http.FileServer` returns `image/svg+xml` from its built-in MIME table, so SVGs need no
handler change.

- `/assets/brand/north-logo-mark.svg`
- `/assets/brand/north-logo-mark-mono.svg`
- `/assets/brand/favicon.svg`
- `/assets/brand/north-logo-mark.png`
- `/assets/brand/north-mascot.png`
- `/assets/brand/pwa-180.png`
- `/assets/brand/pwa-192.png`
- `/assets/brand/pwa-512.png`
- `/assets/brand/pwa-512-maskable.png`

## Still to do

`north-wordmark.svg` and `north-logo-wordmark.svg` do not exist. The wordmark is custom
letterforms and there is no vector source in this repo — only PNGs and two background JPEGs
in `source/`. Tracing letterforms by eye is a redraw, which NOR-52 explicitly rules out, so
these wait on a Figma/Illustrator export or the name of the typeface. Nothing renders them
today, so nothing is blocked.

## Design notes

- **Logo**: geometric north star, calm OS / guidance — not playful, not corporate.
- **Mascot**: small intelligent companion; Three.js-ready silhouette.
- The PNGs' drop shadow is a raster effect and is deliberately absent from the SVGs. A logo
  carrying its own shadow cannot sit on an arbitrary surface.
