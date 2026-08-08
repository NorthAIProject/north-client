/**
 * The world's colours, taken from North's design tokens rather than restated.
 *
 * Nothing here is a hex literal by choice — the fallbacks exist only for the
 * case where the stylesheet has not applied yet. The tokens are oklch and they
 * move; a hardcoded teal would quietly stop matching the rest of the page.
 */
import { readCSSColor } from "../../shared/css-color.js";

export function readPalette() {
  // Read first and reused as the backdrop below, because `--north-hairline` is
  // translucent — it only means anything once it is sitting on the page colour.
  const backdrop = getComputedStyle(document.documentElement)
    .getPropertyValue("--background")
    .trim() || "#0b0e12";

  return {
    // The page behind the canvas. Used for fog, so the line dissolves into the
    // same colour the sections sit on instead of into an arbitrary grey.
    background: readCSSColor("--background", 0x0b0e12),
    // Wayfinding. Signal is the product's own "this is the path" colour.
    signal: readCSSColor("--north-signal", 0x3aa7c4),
    // The coach speaking. Reserved, per the note in input.css — it marks the
    // waypoints the visitor has already passed, never used as decoration.
    agent: readCSSColor("--north-agent", 0x7b62d9),
    // Structure that is present but is not being pointed at. Flattened onto the
    // page colour: the raw token is 8% white, and taken literally it would paint
    // every unlit waypoint bright white.
    hairline: readCSSColor("--north-hairline", 0x2a3038, backdrop),
  };
}
