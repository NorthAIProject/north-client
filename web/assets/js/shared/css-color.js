/**
 * Reading North's design tokens into three.js.
 *
 * Shared because more than one scene needs it and the implementation is not
 * obvious: North's tokens are `oklch()`, and `THREE.Color.setStyle()` only
 * understands hex, `rgb()`, `hsl()`, and named colours. Rather than ship an
 * oklch parser, this hands the string to the one colour parser every browser
 * already has — the 2D canvas — and reads back what it painted.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

/**
 * Resolves a CSS custom property on :root to a THREE.Color.
 *
 * Some of North's tokens are translucent — `--north-hairline` is
 * `oklch(1 0.01 250 / 8%)`, which is to say "the page background, barely
 * lightened". three.js has no notion of a material colour that means that, so
 * the token is composited over `backdrop` first and the flattened result is
 * what comes back. Read a hairline without a backdrop and you get pure white,
 * which is the opposite of what the token is for.
 *
 * The "#000" sentinel means a value the canvas cannot parse (unlikely, but cheap
 * to guard) leaves the result visibly black rather than silently reusing whatever
 * the previous fill style happened to be.
 *
 * @param {string} varName     e.g. "--north-signal"
 * @param {number} fallbackHex used when the property is unset
 * @param {string} backdrop    CSS colour a translucent token is flattened onto
 * @returns {THREE.Color}
 */
export function readCSSColor(varName, fallbackHex, backdrop = "#000") {
  const color = new THREE.Color(fallbackHex);
  const raw = getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
  if (!raw) return color;
  const ctx = document.createElement("canvas").getContext("2d");
  ctx.fillStyle = "#000";
  ctx.fillStyle = backdrop;
  ctx.fillRect(0, 0, 1, 1);
  ctx.fillStyle = "#000";
  ctx.fillStyle = raw;
  ctx.fillRect(0, 0, 1, 1);
  const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
  color.setRGB(r / 255, g / 255, b / 255, THREE.SRGBColorSpace);
  return color;
}
