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

// ------------------------------------------------------------------
// The layout grid.
//
// Here rather than in terrain.js because these are numbers, and terrain.js
// needs three.js to do anything with them. Re-exported from there, which is
// still where anyone building geometry will look for them.

export const COLUMN_SIZE = 0.8; // world units, one day
export const DAY_PITCH = 1.0; // centre to centre across the week
export const WEEK_PITCH = 1.0; // centre to centre between weeks

// The height curve.
//
// PLATE_HEIGHT is what a rest day stands at: not zero, because a rest day is
// still ground and a plane of exactly zero height z-fights with whatever is
// drawn under it.
export const PLATE_HEIGHT = 0.06;

// MAX_HEIGHT is what a day at the top of the scale reaches.
//
// Read against WEEK_PITCH, which is 1: a ceiling of five meant the hardest day
// stood five weeks tall, and a single one of them hid everything behind it. The
// curve below is unchanged — only the ceiling moved — so two columns still
// compare the way they always did.
export const MAX_HEIGHT = 3.2;

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

// ------------------------------------------------------------------
// Framing.
//
// The camera used to stand at two written-down numbers — 9.5 back and 6.2 up —
// chosen against a canvas that filled the viewport. They were wrong even
// there: the top of the frame sat about 17 degrees below the horizon while a
// full-height column reached 8, so every hard day was cut off above the
// shoulder. And a fixed rig cannot be right for two canvas shapes at once.
//
// PITCH is how far above the horizon the camera sits, and it decides how much
// of the landscape can be read. Shallow, a tall Tuesday hides every Tuesday
// behind it. AIM is what it looks at: the ground put half the frame on empty
// floor and pushed the columns into the top of it.

export const FOV = 38; // vertical field of view, degrees
export const PITCH = 0.75; // ~43° above the horizon
export const AIM = MAX_HEIGHT * 0.4;

// VISIBLE_WEEKS is how much history should be in shot at once. A page of
// terrain is eight weeks, and framing for that opens the page on the whole of
// what was fetched rather than on a corner of it.
export const VISIBLE_WEEKS = 8;

// MARGIN is breathing room around the content. At 1 the tallest column sits
// exactly on the frame edge, which reads as clipping even when it is not.
const MARGIN = 1.12;

// The content box, measured from the aim point.
const HALF_WIDTH = 3.5 * DAY_PITCH + COLUMN_SIZE / 2;
const HALF_HEIGHT =
  (MAX_HEIGHT * Math.cos(PITCH) + VISIBLE_WEEKS * WEEK_PITCH * Math.sin(PITCH)) / 2;

/**
 * The distance at which the content box fits the frustum.
 *
 * Solved twice — once against the vertical field of view, once against the
 * horizontal one, which depends on the aspect — and the larger wins. A short
 * wide banner is bound by its height; a narrow one by its width.
 */
export function frame(aspect) {
  const halfV = (FOV * Math.PI) / 360;
  const tanV = Math.tan(halfV);
  const tanH = tanV * Math.max(aspect, 0.1);

  return Math.max((HALF_HEIGHT * MARGIN) / tanV, (HALF_WIDTH * MARGIN) / tanH);
}

/** Where the camera stands for a given aspect, as height and ground distance. */
export function cameraStance(aspect) {
  const distance = frame(aspect);
  return {
    distance,
    height: AIM + distance * Math.sin(PITCH),
    ground: distance * Math.cos(PITCH),
  };
}
