/**
 * The route drawn on a day's column.
 *
 * One module, because this is the seam the elevation ribbons arrive through.
 * Today a route is a flat line laid on the cap of its column, at a constant
 * height. When per-activity altitude streams exist, a second builder here
 * takes the same projected points with a per-point height and returns a
 * ribbon instead — same projection, same fit, different Y source. The scene
 * asks routes.build(...) and knows nothing more than that.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

import { decodePolyline, projectRoute, fitToCap } from "./geo.js";
import { colorFor } from "./palette.js";

// How much of a column's cap a route is allowed to cover. Short of the edge,
// so the line reads as sitting on the day rather than spilling off it.
const ROUTE_SIZE = 0.62;

// Lifted clear of the cap so the line does not z-fight with the surface it is
// drawn on.
const ROUTE_LIFT = 0.012;

/**
 * Builds the line for one route, or null when there is no shape to draw.
 *
 * An indoor session returns null rather than a placeholder ring. The old
 * scene drew a ring for every gym session, which on a real account meant a
 * field of identical circles saying nothing — here the column itself already
 * carries the session's load, so there is nothing left for a marker to add.
 */
export function build(route, { palette, materials, size = 1 }) {
  const points = fitToCap(projectRoute(decodePolyline(route.polyline)), ROUTE_SIZE * size);
  if (points.length < 2) return null;

  const geometry = new THREE.BufferGeometry().setFromPoints(
    points.map(([x, z]) => new THREE.Vector3(x, ROUTE_LIFT, -z)),
  );

  const line = new THREE.Line(geometry, materialFor(materials, palette, route.family));
  line.userData.geometry = geometry;
  return line;
}

// Materials are shared across every route of a family and owned by the scene,
// never by a week. Disposing one when a week scrolls out of range would leave
// every later week of that sport rendering black.
function materialFor(materials, palette, family) {
  if (!materials[family]) {
    materials[family] = new THREE.LineBasicMaterial({ color: colorFor(palette, family) });
  }
  return materials[family];
}
