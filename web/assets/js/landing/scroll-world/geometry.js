/**
 * The bearing line and everything strung along it.
 *
 * The landing page sells continuity: the argument is that North is still useful
 * in month eight, and the page is laid out along a time axis to say so. This is
 * that axis made literal — one path receding into fog, a waypoint for each
 * section, and everything already passed still lit behind the camera.
 *
 * Which is why waypoints are found from `main section` rather than from a list
 * of names in here. Someone editing the copy adds a section and gets a waypoint;
 * nobody has to know this file exists.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";
import { createStarGeometry } from "../../shared/north-star-geometry.js";

// World units. The camera covers all of it over one full page scroll, so this is
// really a statement about how fast travel feels: longer reads as faster.
export const PATH_LENGTH = 230;

/**
 * The path itself. A slow sway rather than a straight line — a straight run down
 * -Z gives no parallax at all, and the world stops reading as a space.
 */
export function createBearingPath() {
  const points = [];
  const segments = 24;
  for (let i = 0; i <= segments; i++) {
    const z = -(i / segments) * PATH_LENGTH;
    points.push(
      new THREE.Vector3(
        Math.sin(i * 0.55) * 6.5,
        Math.cos(i * 0.4) * 2.4,
        z,
      ),
    );
  }
  return new THREE.CatmullRomCurve3(points, false, "catmullrom", 0.4);
}

const LINE_SEGMENTS = 400;

/**
 * The line, drawn as a single 1px polyline.
 *
 * Not a tube. A hairline is North's own vocabulary for "structure that is present
 * but is not the subject" — every panel on the page is bounded by one — and a
 * solid extruded pipe through the middle of the composition would be shouting.
 *
 * The part already travelled is drawn in signal and the part ahead in hairline.
 * That one distinction is what makes the whole composition legible: without it
 * the trail behind the camera and the path in front are the same thread, and
 * "look how much of this is already behind you" is not visible at all.
 */
export function createLine(curve, palette) {
  const geometry = new THREE.BufferGeometry().setFromPoints(curve.getPoints(LINE_SEGMENTS));
  const count = LINE_SEGMENTS + 1;
  const colors = new Float32Array(count * 3);
  geometry.setAttribute("color", new THREE.BufferAttribute(colors, 3));

  const material = new THREE.LineBasicMaterial({
    vertexColors: true,
    transparent: true,
    opacity: 0.9,
  });
  const line = new THREE.Line(geometry, material);
  line.frustumCulled = false;

  let boundary = -1;

  return {
    line,
    /**
     * @param {number} progress 0..1 along the path
     */
    update(progress) {
      // Recoloured only when the boundary actually moves to a new vertex.
      // Rewriting 1200 floats every frame to describe the same picture is the
      // kind of cost that never appears in a profile as one obvious line.
      const next = Math.round(clamp01(progress) * LINE_SEGMENTS);
      if (next === boundary) return;
      boundary = next;
      for (let i = 0; i < count; i++) {
        const c = i <= boundary ? palette.signal : palette.hairline;
        colors[i * 3] = c.r;
        colors[i * 3 + 1] = c.g;
        colors[i * 3 + 2] = c.b;
      }
      geometry.attributes.color.needsUpdate = true;
    },
    dispose() {
      geometry.dispose();
      material.dispose();
    },
  };
}

/**
 * Waypoints: one extruded North mark per section, sitting on the line at that
 * section's own scroll position.
 *
 * A single InstancedMesh because there are ten of them and they share everything
 * but a transform and a colour. `setColorAt` is only called when the count of
 * passed waypoints actually changes — recolouring sixteen instances every frame
 * to say nothing new is the kind of cost that never shows up in a profile as one
 * obvious line.
 */
export function createWaypoints(curve, fractions, palette) {
  const count = fractions.length;
  const geometry = createStarGeometry(0.75, 0.1);
  const material = new THREE.MeshBasicMaterial({
    transparent: true,
    opacity: 0.9,
  });
  const mesh = new THREE.InstancedMesh(geometry, material, count);
  mesh.instanceMatrix.setUsage(THREE.StaticDrawUsage);
  mesh.frustumCulled = false; // the path is long and thin; culling by its bounds is wrong

  const matrix = new THREE.Matrix4();
  const position = new THREE.Vector3();
  const quaternion = new THREE.Quaternion();
  const scale = new THREE.Vector3(1, 1, 1);
  const euler = new THREE.Euler();

  fractions.forEach((t, i) => {
    curve.getPointAt(clamp01(t), position);
    // Face the oncoming camera, then tilt each one differently so travelling
    // past a row of them reads as depth rather than as a sprite sheet.
    euler.set(0, 0, i * 0.7);
    quaternion.setFromEuler(euler);
    matrix.compose(position, quaternion, scale);
    mesh.setMatrixAt(i, matrix);
    mesh.setColorAt(i, palette.hairline);
  });
  mesh.instanceMatrix.needsUpdate = true;

  let litCount = -1;

  return {
    mesh,
    /**
     * Lights every waypoint the camera has already reached and leaves it lit.
     * @param {number} progress 0..1 along the path
     */
    update(progress) {
      const lit = fractions.filter((t) => t <= progress + 0.01).length;
      if (lit === litCount) return;
      litCount = lit;
      for (let i = 0; i < count; i++) {
        mesh.setColorAt(i, i < lit ? palette.signal : palette.hairline);
      }
      mesh.instanceColor.needsUpdate = true;
    },
    dispose() {
      geometry.dispose();
      material.dispose();
    },
  };
}

/**
 * Two hairlines running parallel to the bearing line, one either side.
 *
 * Without them the frame is a single thread in a lot of black, and the camera's
 * movement has nothing to register against — a line seen end-on barely changes
 * as you travel it, so the world reads as static. Three parallel runs give the
 * parallax that makes the trip legible as a trip.
 *
 * They are structure, so they stay hairline the whole way. Only the bearing line
 * itself carries the signal colour, because only it is the argument.
 */
export function createRails(curve, palette, offset = 3.4) {
  const geometry = new THREE.BufferGeometry();
  const vertices = [];
  const point = new THREE.Vector3();
  const tangent = new THREE.Vector3();
  const side = new THREE.Vector3();
  const worldUp = new THREE.Vector3(0, 1, 0);
  const steps = 200;

  for (const sign of [-1, 1]) {
    for (let i = 0; i < steps; i++) {
      // Emitted as segments rather than a strip so both rails live in one
      // geometry; a LineSegments pair costs one draw call instead of two.
      for (const t of [i / steps, (i + 1) / steps]) {
        curve.getPointAt(t, point);
        curve.getTangentAt(t, tangent);
        side.crossVectors(tangent, worldUp).normalize().multiplyScalar(offset * sign);
        vertices.push(point.x + side.x, point.y + side.y, point.z + side.z);
      }
    }
  }

  geometry.setAttribute("position", new THREE.Float32BufferAttribute(vertices, 3));
  const material = new THREE.LineBasicMaterial({
    color: palette.hairline,
    transparent: true,
    opacity: 0.85,
  });
  const rails = new THREE.LineSegments(geometry, material);
  rails.frustumCulled = false;
  return rails;
}

/**
 * Corner brackets around each waypoint — the four marks a viewfinder puts at the
 * edges of the thing it is measuring.
 *
 * A star alone at twenty units of depth is a speck. The brackets give each
 * waypoint a size, which is what makes travelling past one read as passing
 * through something rather than as a dot getting briefly larger. They are also
 * the page's own vocabulary: every panel in the sections behind this canvas is
 * bounded by a hairline.
 *
 * All of them in one LineSegments. Ten separate frames would be ten draw calls
 * for a few hundred bytes of vertex data.
 */
export function createBrackets(curve, fractions, palette) {
  const vertices = [];
  const point = new THREE.Vector3();
  const tangent = new THREE.Vector3();
  const side = new THREE.Vector3();
  const up = new THREE.Vector3();
  const worldUp = new THREE.Vector3(0, 1, 0);

  const HALF = 1.6; // half the frame's width
  const ARM = 0.5; // how far each corner runs before it stops

  fractions.forEach((t) => {
    const at = clamp01(t);
    curve.getPointAt(at, point);
    curve.getTangentAt(at, tangent);
    // A frame square-on to the path, so it reads as a gate the camera goes
    // through rather than as a shape lying at an angle in space.
    side.crossVectors(tangent, worldUp).normalize();
    up.crossVectors(side, tangent).normalize();

    for (const sx of [-1, 1]) {
      for (const sy of [-1, 1]) {
        const cx = point.x + side.x * HALF * sx + up.x * HALF * sy;
        const cy = point.y + side.y * HALF * sx + up.y * HALF * sy;
        const cz = point.z + side.z * HALF * sx + up.z * HALF * sy;
        // one arm inward horizontally, one inward vertically
        vertices.push(
          cx, cy, cz,
          cx - side.x * ARM * sx, cy - side.y * ARM * sx, cz - side.z * ARM * sx,
          cx, cy, cz,
          cx - up.x * ARM * sy, cy - up.y * ARM * sy, cz - up.z * ARM * sy,
        );
      }
    }
  });

  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute("position", new THREE.Float32BufferAttribute(vertices, 3));
  const material = new THREE.LineBasicMaterial({
    color: palette.hairline,
    transparent: true,
    opacity: 0.9,
  });
  const lines = new THREE.LineSegments(geometry, material);
  lines.frustumCulled = false;
  return lines;
}

/**
 * The motes. Read them as the material North accumulates — they thicken around
 * the middle of the journey and thin out past the end.
 *
 * Additive blending, unlike the muscle viewer's glow, which had to avoid it: that
 * one renders onto a white card, where adding light does nothing. This canvas is
 * always the page background, which is dark, so additive is what makes a 2px dot
 * look like it is emitting rather than like a speck of dust.
 */
export function createMotes(curve, palette, count = 5200) {
  const positions = new Float32Array(count * 3);
  const colors = new Float32Array(count * 3);
  const point = new THREE.Vector3();

  for (let i = 0; i < count; i++) {
    const t = Math.random();
    curve.getPointAt(t, point);
    const angle = Math.random() * Math.PI * 2;
    // sqrt keeps the distribution even across the disc instead of crowding the
    // axis, which would put a dense stripe right where the line already is.
    const radius = 3 + Math.sqrt(Math.random()) * 15;
    positions[i * 3] = point.x + Math.cos(angle) * radius;
    positions[i * 3 + 1] = point.y + Math.sin(angle) * radius * 0.6;
    positions[i * 3 + 2] = point.z + (Math.random() - 0.5) * 8;

    // Mostly signal, a minority agent. The ratio is the same claim the page makes
    // in words: the coach is present throughout, but it is not the whole picture.
    const color = Math.random() < 0.22 ? palette.agent : palette.signal;
    colors[i * 3] = color.r;
    colors[i * 3 + 1] = color.g;
    colors[i * 3 + 2] = color.b;
  }

  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute("position", new THREE.Float32BufferAttribute(positions, 3));
  geometry.setAttribute("color", new THREE.Float32BufferAttribute(colors, 3));

  const material = new THREE.PointsMaterial({
    size: 0.17,
    sizeAttenuation: true,
    vertexColors: true,
    map: createMoteTexture(),
    transparent: true,
    opacity: 0.9,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });

  const points = new THREE.Points(geometry, material);
  points.frustumCulled = false;
  return points;
}

// A soft dot. Without it PointsMaterial draws hard-edged squares, which at this
// size read as compression artefacts rather than as light.
function createMoteTexture() {
  const size = 64;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = size;
  const ctx = canvas.getContext("2d");
  const gradient = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
  gradient.addColorStop(0, "rgba(255,255,255,1)");
  gradient.addColorStop(0.4, "rgba(255,255,255,0.5)");
  gradient.addColorStop(1, "rgba(255,255,255,0)");
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, size, size);
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

function clamp01(v) {
  return Math.min(1, Math.max(0, v));
}
