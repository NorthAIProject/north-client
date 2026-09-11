/**
 * The activity terrain's colours, taken from North's design tokens rather
 * than restated.
 *
 * There used to be a SPORT_COLORS table of hex literals in the viewer and a
 * matching set of Tailwind classes in the legend, kept in step by hand with
 * nothing asserting the pairing. Reading the tokens instead means the scene,
 * the legend and the rest of the product cannot drift apart — and the terrain
 * now recolours correctly in light mode and follows a live theme switch,
 * which the hardcoded version could not do at all.
 *
 * The hex fallbacks are the previous values, kept only for the moment before
 * the stylesheet has applied. They are not a second source of truth; if one
 * of them is ever what you see, the token lookup failed.
 *
 * Which sport belongs to which family is Go's answer, not this file's — it
 * arrives on the wire as `family`. See internal/fitness/strava/mapping.go.
 */
import { readCSSColor } from "../css-color.js";

// Keep in step with strava.Families. web/fitness/palette_test.go fails if a
// family exists in Go without a token and a line here.
const FALLBACKS = {
  run: 0xe8973c,
  ride: 0x4ea1ff,
  swim: 0x36d1c4,
  walk: 0x7bc86c,
  strength: 0xb98cf0,
  other: 0x9aa4b2,
};

export function readPalette() {
  // Read first and reused as the backdrop below, because --north-sport-rest is
  // the hairline token, which is translucent — it only means anything once it
  // is sitting on the page colour.
  const backdrop =
    getComputedStyle(document.documentElement).getPropertyValue("--background").trim() || "#0b0e12";

  const sport = {};
  for (const [family, fallback] of Object.entries(FALLBACKS)) {
    sport[family] = readCSSColor(`--north-sport-${family}`, fallback);
  }

  return {
    sport,
    // A rest day is structure rather than an activity, so it takes the rule
    // colour. Flattened onto the page: the raw token is 8% white, and taken
    // literally every rest day would be a bright white slab.
    rest: readCSSColor("--north-sport-rest", 0x2a3038, backdrop),
    // The page behind the canvas, used for fog so terrain that is about to be
    // disposed dissolves into the page rather than popping out of existence.
    background: readCSSColor("--background", 0x0b0e12),
  };
}

// colorFor is the one place a family name becomes a colour. An unknown family
// is drawn neutral rather than skipped: a session that happened must appear.
export function colorFor(palette, family) {
  return palette.sport[family] || palette.sport.other;
}
