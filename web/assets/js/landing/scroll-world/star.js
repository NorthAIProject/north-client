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

const POINTS = 8;
const CARDINAL = 1.0; // long points, at 0/90/180/270°
const DIAGONAL = 0.52; // short points, at 45/135/225/315°
const WAIST = 0.17; // the valley between any two points

/**
 * @param {number} radius  distance from centre to a cardinal tip
 * @param {number} depth   extrusion along Z; 0 gives a flat shape
 * @returns {THREE.BufferGeometry} centred on the origin, facing +Z
 */
export function createStarGeometry(radius = 1, depth = 0.08) {
  const shape = new THREE.Shape();

  // Sixteen vertices around the circle: tip, waist, tip, waist… The tips
  // alternate between the long and short radius, which is what makes it read as
  // a compass rose rather than a generic star.
  const step = (Math.PI * 2) / (POINTS * 2);
  for (let i = 0; i < POINTS * 2; i++) {
    const isTip = i % 2 === 0;
    const isCardinal = (i / 2) % 2 === 0;
    const r = radius * (isTip ? (isCardinal ? CARDINAL : DIAGONAL) : WAIST);
    const angle = i * step - Math.PI / 2; // start at north, not east
    const x = Math.cos(angle) * r;
    const y = Math.sin(angle) * r;
    if (i === 0) shape.moveTo(x, y);
    else shape.lineTo(x, y);
  }
  shape.closePath();

  if (depth <= 0) return new THREE.ShapeGeometry(shape);

  const geometry = new THREE.ExtrudeGeometry(shape, {
    depth,
    bevelEnabled: false,
    curveSegments: 1, // every edge is straight; segments here would be waste
  });
  // ExtrudeGeometry builds from z=0 forward. Centre it so the star rotates about
  // itself rather than about its own back face.
  geometry.translate(0, 0, -depth / 2);
  return geometry;
}
