/**
 * The terrain's arithmetic: polylines, projection, and the height curve.
 *
 * No three.js import, and that is a structural rule rather than an accident.
 * Everything here is a pure function over numbers, which is what lets it be
 * tested under `node --test` with no renderer, no canvas and no DOM. If this
 * file ever needs a THREE type, the type is wrong — convert at the boundary
 * in the module that builds geometry.
 */

/**
 * Decodes a Google-encoded polyline into [lat, lng] pairs.
 *
 * Strava's summary_polyline uses the same algorithm Google Maps does: each
 * coordinate is a zig-zag-encoded delta from the previous one, in five-bit
 * chunks with a continuation bit. Written out rather than pulled from a
 * package because it is twenty lines and the alternative is a dependency.
 */
export function decodePolyline(encoded) {
  if (!encoded) return [];

  const points = [];
  let index = 0;
  let lat = 0;
  let lng = 0;

  while (index < encoded.length) {
    let result = 0;
    let shift = 0;
    let byte;

    do {
      // A truncated string runs off the end and charCodeAt returns NaN.
      // Stopping here leaves the points decoded so far, which is a better
      // answer than looping forever on a corrupt payload.
      if (index >= encoded.length) return points;
      byte = encoded.charCodeAt(index++) - 63;
      result |= (byte & 0x1f) << shift;
      shift += 5;
    } while (byte >= 0x20);
    lat += result & 1 ? ~(result >> 1) : result >> 1;

    result = 0;
    shift = 0;
    do {
      if (index >= encoded.length) return points;
      byte = encoded.charCodeAt(index++) - 63;
      result |= (byte & 0x1f) << shift;
      shift += 5;
    } while (byte >= 0x20);
    lng += result & 1 ? ~(result >> 1) : result >> 1;

    points.push([lat / 1e5, lng / 1e5]);
  }

  return points;
}

/**
 * Projects lat/lng onto a local plane, centred on the route itself.
 *
 * An equirectangular projection with a cos(latitude) correction on longitude.
 * Accurate enough over the few kilometres a single activity covers, and it
 * keeps a route's shape honest — without the correction, everything drawn
 * away from the equator looks stretched sideways.
 */
export function projectRoute(points) {
  if (points.length === 0) return [];

  let latSum = 0;
  let lngSum = 0;
  for (const [lat, lng] of points) {
    latSum += lat;
    lngSum += lng;
  }
  const latCenter = latSum / points.length;
  const lngCenter = lngSum / points.length;
  const lngScale = Math.cos((latCenter * Math.PI) / 180);

  return points.map(([lat, lng]) => [(lng - lngCenter) * lngScale, lat - latCenter]);
}

/**
 * Scales a projected route to fit the cap of a day's column, preserving
 * aspect ratio so a long thin out-and-back is not squashed into a square.
 */
export function fitToCap(projected, size) {
  if (projected.length === 0) return [];

  let minX = Infinity;
  let maxX = -Infinity;
  let minY = Infinity;
  let maxY = -Infinity;
  for (const [x, y] of projected) {
    if (x < minX) minX = x;
    if (x > maxX) maxX = x;
    if (y < minY) minY = y;
    if (y > maxY) maxY = y;
  }

  const spanX = maxX - minX;
  const spanY = maxY - minY;
  const span = Math.max(spanX, spanY);

  // A treadmill run has a polyline of one repeated point, or none. Dividing
  // by a zero span would put NaN into a buffer, which three.js renders as
  // nothing at all and reports as nothing at all.
  if (span <= 0) return projected.map(() => [0, 0]);

  const scale = size / span;
  const offsetX = (minX + maxX) / 2;
  const offsetY = (minY + maxY) / 2;

  return projected.map(([x, y]) => [(x - offsetX) * scale, (y - offsetY) * scale]);
}

// The height curve.
//
// PLATE_HEIGHT is what a rest day stands at: not zero, because a rest day is
// still ground and a plane of exactly zero height z-fights with whatever is
// drawn under it.
export const PLATE_HEIGHT = 0.06;

// MAX_HEIGHT is what a day at the top of the scale reaches.
export const MAX_HEIGHT = 5.0;

/**
 * Turns a day's MET-minutes into a column height.
 *
 * Soft-clipped rather than linear. The scale is the 90th percentile of active
 * days, so roughly one day in ten is above it, and a linear curve would let a
 * six-hour hike shoot several times the height of everything else — which is
 * the failure the elevation-based version of this scene already had, where one
 * big climb flattened the whole field.
 *
 * 1 - exp(-x) reaches 63% of full height at the scale itself and approaches
 * the ceiling without ever crossing it, so an outlier reads as "off the top"
 * rather than as an arbitrary spike. clipped() is how the scene knows to draw
 * that differently, because a curve nobody can see is a lie told in 3D.
 */
export function loadToHeight(loadMETMin, scale) {
  if (!(loadMETMin > 0)) return PLATE_HEIGHT;
  if (!(scale > 0)) return PLATE_HEIGHT;

  const normalised = 1 - Math.exp(-loadMETMin / scale);
  // 1 - exp(-1) is the value at the scale itself; dividing by it puts an
  // average-hard day at a readable fraction of the ceiling rather than at 63%.
  const atScale = 1 - Math.exp(-1);
  const height = (normalised / atScale) * (MAX_HEIGHT * 0.72);

  return Math.max(PLATE_HEIGHT, Math.min(height, MAX_HEIGHT));
}

/** clipped reports whether a day is above the scale and should say so. */
export function clipped(loadMETMin, scale) {
  return scale > 0 && loadMETMin > scale;
}
