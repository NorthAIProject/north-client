# North brand assets

Transparent files for light and dark UI shells. Backgrounds are fully transparent so the
surrounding theme shows through — never place these on a plate.

## Files

| File | Use |
|------|-----|
| `khepri-logo-mark.png` | **The mark.** Gold-and-navy scarab with sun. What `ui.BrandMark` serves, and the PNG tab fallback. |
| `north-logo-mark.svg` | Retired North star. Kept for the landing scroll-world geometry, not used in chrome. |
| `north-logo-mark-mono.svg` | Retired single-colour star. Unused. |
| `favicon.svg` | Tab icon. Simplified scarab silhouette, opaque, `prefers-color-scheme` for dark tab strips. |
| `north-logo-mark.png` | Retired star raster. Unused in chrome. |
| `pwa-180.png` | iOS apple-touch-icon. Opaque `#1C1C1F` plate. |
| `pwa-192.png` | Manifest `any` 192. Opaque `#1C1C1F` plate. |
| `pwa-512.png` | Manifest `any` 512. Opaque `#1C1C1F` plate. |
| `pwa-512-maskable.png` | Manifest `maskable` 512. Same plate, scarab inset for Android adaptive crop. |
| `north-wordmark.png` | App name. Dark ink. **Currently unused** — see below. |
| `north-logo-wordmark.png` | Symbol + name lockup. **Currently unused** — see below. |
| `khepri-mascot.png` | Companion cutout. Live companion (`shared/mascot/`) and the landing scroll-world billboard (`scroll-world/panel.js`). |
| `khepri-mark.png` | Scarab + sun emblem. Stored, not wired into chrome. |
| `khepri-app-icon.png` | Rounded-square app icon cropped from the lockup. Stored, not swapped onto PWA plates. |

Originals with backgrounds live in `source/` for design reference only — never serve them. The retired cube companion is `source/north-mascot-cube.png`. Khepri source stills: `khepri-mascot-original.jpg`, `khepri-emblem-original.jpg`, `khepri-app-icon-lockup.jpg`, `khepri-brand-board.png`.

## Theme strategy

**The mark carries fixed brand colour on both themes.** The scarab is gold on navy,
which reads on the dark shell and on a light surface without a theme swap.

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

The landing scroll-world's waypoints draw that geometry. Redrawing the mark moves the
SVG, the PNG, and the 3D star together.

## Serving

Embedded via `web/assets` (`//go:embed brand …`); new files are picked up with no wiring.
Go's `http.FileServer` returns `image/svg+xml` from its built-in MIME table, so SVGs need no
handler change.

- `/assets/brand/favicon.svg`
- `/assets/brand/khepri-logo-mark.png`
- `/assets/brand/khepri-mascot.png`
- `/assets/brand/khepri-mark.png`
- `/assets/brand/khepri-app-icon.png`
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

- **Logo**: Khepri scarab, gold on navy, sun overhead.
- **Mascot**: Khepri, the navy-and-gold scarab companion. Poses are CSS, not WebGL.
- The PNGs' drop shadow is a raster effect and is deliberately absent from the SVGs. A logo
  carrying its own shadow cannot sit on an arbitrary surface.
