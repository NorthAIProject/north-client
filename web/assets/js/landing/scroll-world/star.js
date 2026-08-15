/**
 * The North mark, as geometry.
 *
 * `brand/north-logo-mark.png` is an eight-point star: four long cardinal points
 * and four short diagonals. It is rebuilt here rather than mapped on as a
 * texture, because a waypoint sits anywhere between two metres and eighty from
 * the camera and a 256px PNG looks like a smudge at one end of that range and
 * like a JPEG at the other. Geometry costs nothing to download and is exact at
 * every distance.
 *
 * If the brand mark is ever redrawn, this is the file that has to follow it.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

// Measured off brand/north-logo-mark.png rather than estimated, and shared with
// web/assets/brand/north-logo-mark.svg, which draws the same four numbers as a
// path. Change these and change that file, or the landing star and the logo
// drift apart — which is what had happened: this file used to model a symmetric
// star with its long points on the cardinals, and the mark has never been that.
//
// The real mark is eight even points with a single long needle laid across one
// diagonal, so it is built here as two shapes rather than one ring of vertices.
const POINTS = 8;
const TIP = 0.63; // every point of the body, evenly
const WAIST = 0.25; // the valley between two points
const NEEDLE_LONG = 1.0; // the needle, up-right
const NEEDLE_SHORT = 0.92; // and its shorter half, down-left
const NEEDLE_HALF_DEG = 6.5; // how wide the needle is at its base
const NEEDLE_BASE = 0.22;
const NEEDLE_AXIS = Math.PI / 4; // 45°, where the long ray sits

/**
 * @param {number} radius  distance from centre to the needle's long tip
 * @param {number} depth   extrusion along Z; 0 gives a flat shape
 * @returns {THREE.BufferGeometry} centred on the origin, facing +Z
 */
export function createStarGeometry(radius = 1, depth = 0.08) {
  const shape = new THREE.Shape();

  // The body: sixteen vertices, tip and waist alternating. Every tip is the
  // same length — the asymmetry people see in the mark comes from the needle
  // below, not from uneven points.
  const step = (Math.PI * 2) / (POINTS * 2);
  for (let i = 0; i < POINTS * 2; i++) {
    const r = radius * (i % 2 === 0 ? TIP : WAIST);
    const angle = i * step;
    const x = Math.cos(angle) * r;
    const y = Math.sin(angle) * r;
    if (i === 0) shape.moveTo(x, y);
    else shape.lineTo(x, y);
  }
  shape.closePath();

  // The needle is its own shape rather than more vertices on the body's ring.
  // Adding a second contour to one THREE.Shape would be read as a hole and cut
  // the needle *out* of the star; both geometry constructors below take an
  // array of shapes and union them, which is what is wanted.
  const spread = (NEEDLE_HALF_DEG * Math.PI) / 180;
  const needle = new THREE.Shape();
  const at = (ang, r) =>
    [Math.cos(ang) * radius * r, Math.sin(ang) * radius * r];
  const [tx, ty] = at(NEEDLE_AXIS, NEEDLE_LONG);
  const [lx, ly] = at(NEEDLE_AXIS + spread, NEEDLE_BASE);
  const [bx, by] = at(NEEDLE_AXIS + Math.PI, NEEDLE_SHORT);
  const [rx, ry] = at(NEEDLE_AXIS - spread, NEEDLE_BASE);
  needle.moveTo(tx, ty);
  needle.lineTo(lx, ly);
  needle.lineTo(bx, by);
  needle.lineTo(rx, ry);
  needle.closePath();

  if (depth <= 0) return new THREE.ShapeGeometry([shape, needle]);

  const geometry = new THREE.ExtrudeGeometry([shape, needle], {
    depth,
    bevelEnabled: false,
    curveSegments: 1, // every edge is straight; segments here would be waste
  });
  // ExtrudeGeometry builds from z=0 forward. Centre it so the star rotates about
  // itself rather than about its own back face.
  geometry.translate(0, 0, -depth / 2);
  return geometry;
}
