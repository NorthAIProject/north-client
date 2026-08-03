/**
 * The muscle viewer on the marketing page.
 *
 * The figure is built from primitives rather than loaded from a model file: it
 * has to communicate which region is working, not what a body looks like, and a
 * few dozen capsules do that at a fraction of the weight. Every mesh is tagged
 * with a muscle key, and the shares come from the readout beside the canvas, so
 * the numbers a visitor reads are the numbers being coloured.
 *
 * To swap in a real anatomical model later, replace buildFigure() with a loader
 * that returns the same { group, regions } shape. Nothing else changes.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

// The figure has to sit on the panel in both themes: a single mid grey reads as
// a silhouette on a light card and disappears on a dark one.
const THEME = {
  dark: { base: 0x3a4048, inert: 0x272c33 },
  light: { base: 0xb4bcc6, inert: 0xd4d9df },
};
const EMBER = 0xe8973c;

export function createViewer(canvas, options = {}) {
  const reduced = Boolean(options.reduced);
  let palette = options.dark === false ? THEME.light : THEME.dark;

  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: true,
    alpha: true,
    powerPreference: "low-power",
  });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(35, 1, 0.1, 100);
  camera.position.set(0, 0.25, 9.2);
  camera.lookAt(0, 0.1, 0);

  scene.add(new THREE.HemisphereLight(0xdfe7f2, 0x0b0d10, 1.15));

  const key = new THREE.DirectionalLight(0xffffff, 1.5);
  key.position.set(3.5, 5, 6);
  scene.add(key);

  const rim = new THREE.DirectionalLight(0x8ec6ff, 0.7);
  rim.position.set(-4, 1.5, -5);
  scene.add(rim);

  const { group, regions, inert } = buildFigure(palette);
  scene.add(group);

  // ---------------------------------------------------------------------
  // Interaction: a drag to rotate is the whole control surface, so pulling in
  // OrbitControls would double the download for one axis.
  // ---------------------------------------------------------------------
  let targetY = 0.35;
  let currentY = 0.35;
  let dragging = false;
  let lastX = 0;

  canvas.addEventListener("pointerdown", (e) => {
    dragging = true;
    lastX = e.clientX;
    canvas.setPointerCapture(e.pointerId);
  });

  canvas.addEventListener("pointermove", (e) => {
    if (!dragging) return;
    targetY += (e.clientX - lastX) * 0.01;
    lastX = e.clientX;
  });

  const release = (e) => {
    dragging = false;
    if (e.pointerId !== undefined && canvas.hasPointerCapture(e.pointerId)) {
      canvas.releasePointerCapture(e.pointerId);
    }
  };
  canvas.addEventListener("pointerup", release);
  canvas.addEventListener("pointercancel", release);

  // ---------------------------------------------------------------------
  // Sizing
  // ---------------------------------------------------------------------
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

  // ---------------------------------------------------------------------
  // Render loop, paused whenever the canvas is off screen
  // ---------------------------------------------------------------------
  let running = true;
  let frame = 0;

  const visibility = new IntersectionObserver((entries) => {
    running = entries.some((e) => e.isIntersecting);
    if (running) frame = requestAnimationFrame(tick);
  });
  visibility.observe(canvas);

  function tick() {
    if (!running) return;
    if (!dragging && !reduced) targetY += 0.0022;
    currentY += (targetY - currentY) * 0.08;
    group.rotation.y = currentY;
    renderer.render(scene, camera);
    frame = requestAnimationFrame(tick);
  }
  frame = requestAnimationFrame(tick);

  // ---------------------------------------------------------------------
  // Highlighting
  // ---------------------------------------------------------------------
  const base = new THREE.Color(palette.base);
  const ember = new THREE.Color(EMBER);
  let current = [];

  function setLoads(loads) {
    current = loads;
    const peak = loads.reduce((m, l) => Math.max(m, l.share), 0) || 1;

    for (const material of Object.values(regions)) {
      material.color.copy(base);
      material.emissive.copy(base);
      material.emissiveIntensity = 0;
    }

    for (const load of loads) {
      const material = regions[load.key];
      if (!material) continue;
      const intensity = Math.min(1, load.share / peak);
      material.color.copy(base).lerp(ember, 0.3 + 0.7 * intensity);
      material.emissive.copy(ember);
      material.emissiveIntensity = 0.12 + 0.38 * intensity;
    }
  }

  return {
    setLoads,
    setTheme(isDark) {
      palette = isDark ? THEME.dark : THEME.light;
      base.set(palette.base);
      inert.color.set(palette.inert);
      setLoads(current);
    },
    destroy() {
      cancelAnimationFrame(frame);
      running = false;
      resizeObserver.disconnect();
      visibility.disconnect();
      renderer.dispose();
    },
  };
}

/**
 * Builds the figure and returns the material used for each muscle key, so
 * colouring a region never has to walk the scene graph.
 */
function buildFigure(palette) {
  const group = new THREE.Group();
  const regions = {};
  const inert = new THREE.MeshStandardMaterial({
    color: palette.inert,
    roughness: 0.85,
    metalness: 0.05,
  });

  function materialFor(key) {
    if (!regions[key]) {
      regions[key] = new THREE.MeshStandardMaterial({
        color: palette.base,
        emissive: palette.base,
        emissiveIntensity: 0,
        roughness: 0.55,
        metalness: 0.1,
      });
    }
    return regions[key];
  }

  function add(key, geometry, position, rotation) {
    const mesh = new THREE.Mesh(geometry, key ? materialFor(key) : inert);
    mesh.position.set(position[0], position[1], position[2]);
    if (rotation) mesh.rotation.set(rotation[0], rotation[1], rotation[2]);
    group.add(mesh);
    return mesh;
  }

  // Mirrors a part across the sagittal plane, which is how every limb is built.
  function pair(key, geometry, position, rotation) {
    add(key, geometry, position, rotation);
    add(
      key,
      geometry,
      [-position[0], position[1], position[2]],
      rotation ? [rotation[0], -rotation[1], -rotation[2]] : null,
    );
  }

  const capsule = (r, l) => new THREE.CapsuleGeometry(r, l, 6, 14);

  // Head and neck — structural, never highlighted.
  add(null, new THREE.SphereGeometry(0.34, 24, 18), [0, 2.52, 0]);
  add(null, capsule(0.14, 0.2), [0, 2.13, 0]);

  // Trunk core: a slab the muscle plates sit on.
  add(null, new THREE.BoxGeometry(1.0, 1.45, 0.5), [0, 1.28, 0]);
  add(null, new THREE.BoxGeometry(0.94, 0.42, 0.52), [0, 0.36, 0]);

  // Front of the trunk.
  pair("traps", capsule(0.13, 0.34), [0.3, 1.94, 0.03], [0, 0, Math.PI / 2.4]);
  add(null, new THREE.BoxGeometry(0.86, 0.42, 0.16), [0, 1.72, 0.28]);
  add("abs", new THREE.BoxGeometry(0.5, 0.72, 0.16), [0, 1.04, 0.27]);
  pair("serratus", capsule(0.08, 0.3), [0.42, 1.34, 0.18], [Math.PI / 2.6, 0, 0]);

  // Back of the trunk.
  pair("lats", new THREE.BoxGeometry(0.32, 0.86, 0.16), [0.33, 1.42, -0.27]);
  pair("rhomboids", new THREE.BoxGeometry(0.28, 0.36, 0.14), [0.19, 1.76, -0.27]);
  add("erectors", new THREE.BoxGeometry(0.3, 1.1, 0.16), [0, 1.12, -0.28]);

  // Shoulders and arms.
  pair("delts", new THREE.SphereGeometry(0.26, 20, 14), [0.62, 1.86, 0]);
  pair("biceps", capsule(0.13, 0.42), [0.7, 1.46, 0.09], [0, 0, 0.12]);
  pair("triceps", capsule(0.13, 0.42), [0.72, 1.46, -0.09], [0, 0, 0.12]);
  pair("forearms", capsule(0.11, 0.5), [0.8, 0.86, 0.02], [0, 0, 0.06]);
  pair(null, new THREE.SphereGeometry(0.11, 14, 10), [0.83, 0.52, 0.02]);

  // Hips and legs.
  pair("glutes", new THREE.SphereGeometry(0.28, 20, 14), [0.24, 0.16, -0.22]);
  pair("quads", capsule(0.21, 0.72), [0.28, -0.42, 0.08]);
  pair("hamstrings", capsule(0.18, 0.7), [0.28, -0.44, -0.14]);
  pair("adductors", capsule(0.11, 0.62), [0.11, -0.46, -0.01]);
  pair(null, new THREE.SphereGeometry(0.16, 16, 12), [0.28, -1.02, 0]);
  pair("calves", capsule(0.16, 0.48), [0.28, -1.42, -0.06]);
  pair(null, capsule(0.1, 0.42), [0.28, -1.48, 0.06]);
  pair(null, new THREE.BoxGeometry(0.26, 0.14, 0.52), [0.28, -1.92, 0.1]);

  group.position.y = -0.15;
  return { group, regions, inert };
}
