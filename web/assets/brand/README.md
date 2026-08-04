# North brand assets

Transparent PNGs for light and dark UI shells. Backgrounds are fully transparent so the surrounding theme shows through.

| File | Use |
|------|-----|
| `north-logo-mark.png` | Symbol only (8-point north star). Works on light and dark. |
| `north-wordmark.png` | App name only. Dark ink — use on light backgrounds, or invert for dark (`filter: invert(1)` / dedicated on-dark asset later). |
| `north-logo-wordmark.png` | Symbol + name lockup. Same wordmark constraint as above. |
| `north-mascot.png` | Companion cutout for empty states, onboarding, PWA icons, Three.js reference. |

Originals (with background) live in `source/` for design reference only — do not serve as primary UI assets.

## Serving

Embedded via `web/assets` (`//go:embed brand …`). Public URLs:

- `/assets/brand/north-logo-mark.png`
- `/assets/brand/north-wordmark.png`
- `/assets/brand/north-logo-wordmark.png`
- `/assets/brand/north-mascot.png`

## Design notes

- **Logo**: geometric north star, calm OS / guidance — not playful, not corporate.
- **Mascot**: small intelligent companion; Three.js-ready silhouette (see Linear NOR ticket for living companion).
- Prefer SVG recreation of the mark/wordmark for production crispness at all sizes (follow-up).
