/**
 * The 3D activity landscape.
 *
 * Each recent Strava activity is drawn as its own GPS route, floating at a
 * height proportional to the elevation it climbed, laid out as a grid you can
 * orbit. The two spatial axes are just arrangement; the vertical one carries
 * real information, which is the only reason this is 3D rather than a list of
 * thumbnails.
 *
 * Activities with no GPS (a treadmill run, a gym session) still appear, as a
 * marker rather than a route: dropping them would make someone's week look
 * emptier than it was.
 *
 * Uses the same vendored three.js as the muscle viewer, and the same
 * hand-rolled drag control — OrbitControls is not vendored, and one axis of
 * rotation plus a zoom does not justify the download.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

// Route colour by sport family. Runs, rides and swims are the three a person
// glances at and expects to tell apart instantly; everything else shares the
// neutral tone rather than inventing a colour nobody has learned.
const SPORT_COLORS = {
  run: 0xe8973c,
  ride: 0x4ea1ff,
  swim: 0x36d1c4,
  other: 0x9aa4b2,
};

function sportFamily(sportType) {
  const s = (sportType || "").toLowerCase();
  if (s.includes("run")) return "run";
  if (s.includes("ride") || s.includes("cycl") || s.includes("bike")) return "ride";
  if (s.includes("swim")) return "swim";
  return "other";
}

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
      byte = encoded.charCodeAt(index++) - 63;
      result |= (byte & 0x1f) << shift;
      shift += 5;
    } while (byte >= 0x20);
    lat += result & 1 ? ~(result >> 1) : result >> 1;

    result = 0;
    shift = 0;
    do {
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
function projectRoute(points) {
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

// Scales a projected route to fit a tile, preserving aspect ratio so a long
// thin out-and-back does not get squashed into a square.
function fitToTile(projected, size) {
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
  if (span <= 0) return projected.map(() => [0, 0]);

  const scale = size / span;
  const offsetX = (minX + maxX) / 2;
  const offsetY = (minY + maxY) / 2;

  return projected.map(([x, y]) => [(x - offsetX) * scale, (y - offsetY) * scale]);
}

function hasWebGL() {
  try {
    const probe = document.createElement("canvas");
    return Boolean(
      window.WebGLRenderingContext && (probe.getContext("webgl2") || probe.getContext("webgl")),
    );
  } catch {
    return false;
  }
}

const TILE = 3.2; // world units per activity cell
const ROUTE_SIZE = 2.3; // route drawn slightly inside its cell
const MAX_LIFT = 4.5; // world units for the biggest climb in the set

export async function createViewer(canvas, activities, options = {}) {
  if (!hasWebGL()) throw new Error("WebGL unavailable");

  const reduced = Boolean(options.reduced);
  const onSelect = options.onSelect || (() => {});

  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true, powerPreference: "low-power" });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.outputColorSpace = THREE.SRGBColorSpace;

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(42, 1, 0.1, 500);

  scene.add(new THREE.AmbientLight(0xffffff, 1.4));
  const key = new THREE.DirectionalLight(0xffffff, 1.1);
  key.position.set(4, 8, 6);
  scene.add(key);

  // Everything hangs off one group so a single rotation turns the whole
  // landscape, and disposal has one place to walk.
  const group = new THREE.Group();
  scene.add(group);

  // Grid laid out as square-ish, so twenty activities read as a field rather
  // than a queue disappearing toward the horizon.
  const columns = Math.max(1, Math.ceil(Math.sqrt(activities.length)));
  const rows = Math.max(1, Math.ceil(activities.length / columns));

  const peakClimb = activities.reduce((m, a) => Math.max(m, a.elevation_gain_m || 0), 0);

  const pickables = [];
  const disposables = [];

  activities.forEach((activity, i) => {
    const column = i % columns;
    const row = Math.floor(i / columns);

    // Centred on the origin so rotation turns about the middle of the field.
    const originX = (column - (columns - 1) / 2) * TILE;
    const originZ = (row - (rows - 1) / 2) * TILE;
    const lift = peakClimb > 0 ? ((activity.elevation_gain_m || 0) / peakClimb) * MAX_LIFT : 0;

    const color = SPORT_COLORS[sportFamily(activity.sport_type)];
    const cell = new THREE.Group();
    cell.position.set(originX, lift, originZ);
    group.add(cell);

    // The pad sits under every activity, GPS or not: it is the click target,
    // and it gives an indoor session something to be.
    const padGeometry = new THREE.PlaneGeometry(TILE * 0.86, TILE * 0.86);
    const padMaterial = new THREE.MeshBasicMaterial({
      color,
      transparent: true,
      opacity: 0.07,
      side: THREE.DoubleSide,
    });
    const pad = new THREE.Mesh(padGeometry, padMaterial);
    pad.rotation.x = -Math.PI / 2;
    pad.userData = { index: i, baseOpacity: 0.07, material: padMaterial };
    cell.add(pad);
    pickables.push(pad);
    disposables.push(padGeometry, padMaterial);

    // A stem down to the ground plane, so a floating tile reads as height
    // above zero rather than as an arbitrary position.
    if (lift > 0.01) {
      const stemGeometry = new THREE.BufferGeometry().setFromPoints([
        new THREE.Vector3(0, 0, 0),
        new THREE.Vector3(0, -lift, 0),
      ]);
      const stemMaterial = new THREE.LineBasicMaterial({ color, transparent: true, opacity: 0.25 });
      cell.add(new THREE.Line(stemGeometry, stemMaterial));
      disposables.push(stemGeometry, stemMaterial);
    }

    const route = fitToTile(projectRoute(decodePolyline(activity.polyline)), ROUTE_SIZE);

    if (route.length > 1) {
      const points = route.map(([x, z]) => new THREE.Vector3(x, 0.02, -z));
      const routeGeometry = new THREE.BufferGeometry().setFromPoints(points);
      const routeMaterial = new THREE.LineBasicMaterial({ color });
      cell.add(new THREE.Line(routeGeometry, routeMaterial));
      disposables.push(routeGeometry, routeMaterial);
    } else {
      // No GPS. A small ring stands in for the route, so the activity is
      // present and clickable without pretending to a shape it never had.
      const ringGeometry = new THREE.RingGeometry(0.3, 0.38, 24);
      const ringMaterial = new THREE.MeshBasicMaterial({ color, side: THREE.DoubleSide, transparent: true, opacity: 0.8 });
      const ring = new THREE.Mesh(ringGeometry, ringMaterial);
      ring.rotation.x = -Math.PI / 2;
      ring.position.y = 0.02;
      cell.add(ring);
      disposables.push(ringGeometry, ringMaterial);
    }
  });

  // Frame the whole field regardless of how many activities there are.
  const spread = Math.max(columns, rows) * TILE;
  camera.position.set(0, spread * 0.85 + 3, spread * 0.95 + 4);
  camera.lookAt(0, 0, 0);

  // -------------------------------------------------------------------
  // Interaction: drag to turn, wheel to zoom, click to select.
  // -------------------------------------------------------------------
  let targetY = 0.35;
  let currentY = 0.35;
  let dragging = false;
  let lastX = 0;
  let downX = 0;
  let downY = 0;
  let zoom = 1;

  const raycaster = new THREE.Raycaster();
  const pointer = new THREE.Vector2();

  const onPointerDown = (e) => {
    dragging = true;
    lastX = e.clientX;
    downX = e.clientX;
    downY = e.clientY;
    canvas.setPointerCapture(e.pointerId);
  };

  const onPointerMove = (e) => {
    if (!dragging) return;
    targetY += (e.clientX - lastX) * 0.008;
    lastX = e.clientX;
  };

  const onPointerUp = (e) => {
    dragging = false;
    if (e.pointerId !== undefined && canvas.hasPointerCapture(e.pointerId)) {
      canvas.releasePointerCapture(e.pointerId);
    }

    // A click is a pointerup that barely moved; anything else was a drag.
    if (Math.hypot(e.clientX - downX, e.clientY - downY) > 6) return;

    const rect = canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    pointer.x = ((e.clientX - rect.left) / rect.width) * 2 - 1;
    pointer.y = -((e.clientY - rect.top) / rect.height) * 2 + 1;
    raycaster.setFromCamera(pointer, camera);

    const hit = raycaster.intersectObjects(pickables, false)[0];
    select(hit ? hit.object.userData.index : null);
  };

  const onWheel = (e) => {
    e.preventDefault();
    zoom = Math.min(2.2, Math.max(0.45, zoom + e.deltaY * 0.0012));
  };

  canvas.addEventListener("pointerdown", onPointerDown);
  canvas.addEventListener("pointermove", onPointerMove);
  canvas.addEventListener("pointerup", onPointerUp);
  canvas.addEventListener("pointercancel", onPointerUp);
  canvas.addEventListener("wheel", onWheel, { passive: false });

  let selected = null;

  function select(index) {
    selected = index;
    for (const pad of pickables) {
      const { material, baseOpacity, index: i } = pad.userData;
      material.opacity = i === index ? 0.42 : baseOpacity;
    }
    onSelect(index === null ? null : activities[index], index);
  }

  // -------------------------------------------------------------------
  function resize() {
    const width = canvas.clientWidth;
    const height = canvas.clientHeight;
    if (!width || !height) return;
    renderer.setSize(width, height, false);
    camera.aspect = width / height;
    camera.updateProjectionMatrix();
  }

  const resizeObserver = new ResizeObserver(resize);
  resizeObserver.observe(canvas);
  resize();

  let running = true;
  let frame = 0;
  const baseCamera = camera.position.clone();

  const visibility = new IntersectionObserver((entries) => {
    running = entries.some((e) => e.isIntersecting);
    if (running) frame = requestAnimationFrame(tick);
  });
  visibility.observe(canvas);

  function tick() {
    if (!running) return;
    if (!dragging && !reduced) targetY += 0.0015;
    currentY += (targetY - currentY) * 0.08;
    group.rotation.y = currentY;

    camera.position.copy(baseCamera).multiplyScalar(zoom);
    camera.lookAt(0, 0, 0);

    renderer.render(scene, camera);
    frame = requestAnimationFrame(tick);
  }
  frame = requestAnimationFrame(tick);

  return {
    select,
    selected: () => selected,
    destroy() {
      cancelAnimationFrame(frame);
      running = false;
      resizeObserver.disconnect();
      visibility.disconnect();
      canvas.removeEventListener("pointerdown", onPointerDown);
      canvas.removeEventListener("pointermove", onPointerMove);
      canvas.removeEventListener("pointerup", onPointerUp);
      canvas.removeEventListener("pointercancel", onPointerUp);
      canvas.removeEventListener("wheel", onWheel);

      for (const item of disposables) item.dispose();
      scene.clear();
      renderer.dispose();
      renderer.forceContextLoss();
    },
  };
}
