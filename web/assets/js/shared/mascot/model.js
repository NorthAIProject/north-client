/**
 * The North mascot, as geometry (NOR-51).
 *
 * `brand/north-mascot.png` is the 2D source of truth: a soft cube head with one
 * corner cut flat, two large glossy eyes, a thin mouth, the North mark on the
 * forehead, a small torso and four stubby limbs, lit teal and speckled with
 * emissive dots.
 *
 * It is built from primitives here rather than loaded as a GLB. A hand-authored
 * model would be 200-500KB and would drag in GLTFLoader, an AnimationMixer, and
 * the tools/model build step; the shape is simple enough that code costs ~10KB
 * and stays editable in the repo. `buildMascot()` is the seam: swapping in a
 * real GLB later means rewriting this file and nothing else, because scene.js
 * only ever addresses parts by name.
 *
 * No RoundedBoxGeometry: it lives in three's examples, not core, and vendoring
 * it would inherit the cache-immutability hazard in vendor/README.md. An
 * extruded profile with a curved bevel gives the same soft cube and reuses the
 * technique north-star-geometry.js already uses.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

import { readCSSColor } from "../css-color.js";
import { createStarGeometry } from "../north-star-geometry.js";

/**
 * The mascot's colours, taken from North's design tokens rather than restated.
 *
 * Same rule as scroll-world/palette.js: no hex literal is a choice, only a
 * fallback for the moment before the stylesheet applies. Read once at mount —
 * /app/* and the auth shell are hard-coded dark (web/shared/layout/base.templ),
 * which is why nothing here subscribes to `theme-changed`.
 */
export function readMascotPalette() {
  return {
    // The body's lit side, and the emissive that reads as "awake".
    signal: readCSSColor("--north-signal", 0x3aa7c4),
    // The shadowed side and the eyes: near-black navy, straight off the mark's
    // own gradient.
    deep: readCSSColor("--background", 0x0b0e12),
  };
}

/**
 * A rounded square with one corner cut flat, as a 2D profile.
 *
 * The chamfer is the detail that stops the head reading as a generic cube —
 * it is the flat facet on the mascot's upper left in the PNG.
 *
 * @param {number} w      full width
 * @param {number} h      full height
 * @param {number} r      corner radius
 * @param {number} cut    length of the flat chamfer on the upper-left corner
 */
function headProfile(w, h, r, cut) {
  const x = w / 2;
  const y = h / 2;
  const shape = new THREE.Shape();

  shape.moveTo(-x + r, -y);
  shape.lineTo(x - r, -y);
  shape.quadraticCurveTo(x, -y, x, -y + r);
  shape.lineTo(x, y - r);
  shape.quadraticCurveTo(x, y, x - r, y);
  // Upper left: straight across instead of a curve.
  shape.lineTo(-x + cut, y);
  shape.lineTo(-x, y - cut);
  shape.lineTo(-x, -y + r);
  shape.quadraticCurveTo(-x, -y, -x + r, -y);
  shape.closePath();

  return shape;
}

/**
 * Points scattered over the body, for the emissive speckles.
 *
 * One THREE.Points cloud rather than dozens of small meshes: it is a single
 * draw call and needs no texture. Positions are deterministic — a seeded
 * sequence, not Math.random — so the mascot looks identical on every mount
 * instead of shimmering differently for each visitor.
 */
function speckleGeometry(count) {
  const positions = new Float32Array(count * 3);
  let seed = 0x4e4f5254; // "NORT"

  const next = () => {
    // Numerical Recipes LCG. Good enough to scatter dots, and stable.
    seed = (seed * 1664525 + 1013904223) >>> 0;
    return seed / 0xffffffff;
  };

  for (let i = 0; i < count; i++) {
    // Spherical shell around the torso/head mass, pushed outward so dots sit
    // on the surface rather than inside it.
    const theta = next() * Math.PI * 2;
    const phi = Math.acos(2 * next() - 1);
    const radius = 0.55 + next() * 0.75;

    positions[i * 3] = Math.sin(phi) * Math.cos(theta) * radius;
    positions[i * 3 + 1] = 0.75 + Math.cos(phi) * radius * 0.85;
    positions[i * 3 + 2] = Math.sin(phi) * Math.sin(theta) * radius;
  }

  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
  return geometry;
}

/**
 * Builds the mascot.
 *
 * @param {{signal: THREE.Color, deep: THREE.Color}} palette
 * @returns {{root: THREE.Group, parts: Object, materials: Object, dispose: function}}
 */
export function buildMascot(palette) {
  const disposables = [];
  const track = (item) => {
    disposables.push(item);
    return item;
  };

  const body = track(
    new THREE.MeshStandardMaterial({
      color: palette.signal,
      roughness: 0.42,
      metalness: 0.05,
      emissive: palette.signal,
      emissiveIntensity: 0.18,
    }),
  );

  const dark = track(
    new THREE.MeshStandardMaterial({
      color: palette.deep,
      roughness: 0.12,
      metalness: 0.1,
    }),
  );

  const glint = track(
    new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.9 }),
  );

  // Shared by the forehead glyph and the speckles: both are "the light inside
  // it", and animating one intensity moves the whole tell at once.
  const glow = track(
    new THREE.MeshBasicMaterial({ color: palette.signal, transparent: true, opacity: 0.85 }),
  );

  const root = new THREE.Group();

  // --- head -----------------------------------------------------------------
  const headGeometry = track(
    new THREE.ExtrudeGeometry(headProfile(1.5, 1.32, 0.3, 0.42), {
      depth: 1.1,
      bevelEnabled: true,
      bevelThickness: 0.16,
      bevelSize: 0.16,
      bevelSegments: 4,
      curveSegments: 8,
    }),
  );
  headGeometry.center();

  const head = new THREE.Mesh(headGeometry, body);
  head.position.set(0, 1.2, 0);
  root.add(head);

  // --- eyes -----------------------------------------------------------------
  // Parented to the head so a tilt carries them, and scaled on Y for the
  // half-lidded "thinking" pose without any morph target.
  const eyeGeometry = track(new THREE.SphereGeometry(0.29, 20, 16));
  const glintGeometry = track(new THREE.SphereGeometry(0.085, 10, 8));

  const makeEye = (x) => {
    const group = new THREE.Group();
    const ball = new THREE.Mesh(eyeGeometry, dark);
    group.add(ball);

    const highlight = new THREE.Mesh(glintGeometry, glint);
    highlight.position.set(x > 0 ? 0.09 : -0.09, 0.1, 0.24);
    group.add(highlight);

    group.position.set(x, 0.02, 0.5);
    head.add(group);
    return group;
  };

  const eyeL = makeEye(-0.36);
  const eyeR = makeEye(0.36);

  // --- mouth ----------------------------------------------------------------
  const mouthGeometry = track(new THREE.BoxGeometry(0.34, 0.045, 0.04));
  const mouth = new THREE.Mesh(mouthGeometry, dark);
  mouth.position.set(0, -0.42, 0.62);
  head.add(mouth);

  // --- forehead glyph -------------------------------------------------------
  const glyphGeometry = track(createStarGeometry(0.22, 0.03));
  const glyph = new THREE.Mesh(glyphGeometry, glow);
  glyph.position.set(0.16, 0.46, 0.6);
  head.add(glyph);

  // --- torso ----------------------------------------------------------------
  const torsoGeometry = track(new THREE.CapsuleGeometry(0.36, 0.32, 6, 18));
  const torso = new THREE.Mesh(torsoGeometry, body);
  torso.position.set(0, 0.28, 0);
  root.add(torso);

  // --- limbs ----------------------------------------------------------------
  const armGeometry = track(new THREE.CapsuleGeometry(0.115, 0.42, 5, 12));
  const legGeometry = track(new THREE.CapsuleGeometry(0.13, 0.34, 5, 12));

  const makeLimb = (geometry, x, y, tilt) => {
    const limb = new THREE.Mesh(geometry, body);
    limb.position.set(x, y, 0);
    limb.rotation.z = tilt;
    root.add(limb);
    return limb;
  };

  const armL = makeLimb(armGeometry, -0.52, 0.3, 0.55);
  const armR = makeLimb(armGeometry, 0.52, 0.3, -0.55);
  const legL = makeLimb(legGeometry, -0.2, -0.32, 0.12);
  const legR = makeLimb(legGeometry, 0.2, -0.32, -0.12);

  // --- speckles -------------------------------------------------------------
  const speckleMaterial = track(
    new THREE.PointsMaterial({
      color: palette.signal,
      size: 0.075,
      transparent: true,
      opacity: 0.75,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
      sizeAttenuation: true,
    }),
  );
  const speckles = new THREE.Points(track(speckleGeometry(40)), speckleMaterial);
  root.add(speckles);

  return {
    root,
    parts: { head, torso, eyeL, eyeR, armL, armR, legL, legR, glyph, mouth, speckles },
    materials: { body, glow, speckleMaterial },
    dispose() {
      for (const item of disposables) item.dispose();
    },
  };
}
