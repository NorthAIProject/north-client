/**
 * The muscle viewer on the marketing page.
 *
 * The figure is built from primitives (buildFigure) rather than loaded from a model
 * file: it started as a way to communicate which region is working at a fraction of
 * the weight of a real mesh. Every mesh is tagged with a muscle key, and the shares
 * come from the readout beside the canvas, so the numbers a visitor reads are the
 * numbers being coloured.
 *
 * loadFigure() is the seam for a real anatomical model (NOR-6): it must resolve to
 * the same { group, regions, inert } shape buildFigure returns today. Nothing else
 * in this file — setLoads, setTheme, the render loop — needs to change when it does.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";
import { RoomEnvironment } from "/assets/js/vendor/three-room-environment.module.js";

// The figure has to sit on the panel in both themes: a single mid grey reads as
// a silhouette on a light card and disappears on a dark one. These are figure
// shading values with no brand-token counterpart — only the effort colour (ember)
// is a brand token, read live from CSS below.
const THEME = {
  dark: { base: 0x3a4048, inert: 0x272c33 },
  light: { base: 0xb4bcc6, inert: 0xd4d9df },
};

// Resolves a CSS custom property to a THREE.Color through the browser's own color
// parser. Necessary because North's tokens are oklch() and THREE.Color.setStyle()
// only understands hex/rgb()/hsl()/named colors. The "#000" sentinel means a value
// the canvas can't parse (unlikely, but cheap to guard) leaves visibly black rather
// than silently reusing whatever the fallback default was.
function readCSSColor(varName, fallbackHex) {
  const color = new THREE.Color(fallbackHex);
  const raw = getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
  if (!raw) return color;
  const ctx = document.createElement("canvas").getContext("2d");
  ctx.fillStyle = "#000";
  ctx.fillStyle = raw;
  ctx.fillRect(0, 0, 1, 1);
  const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
  color.setRGB(r / 255, g / 255, b / 255, THREE.SRGBColorSpace);
  return color;
}

// A soft radial-gradient disc under the figure. Cheaper than a shadow map by a full
// render pass every frame — the figure never stops rotating (see tick()), so nothing
// about a real shadow could be cached anyway.
function createShadowTexture() {
  const size = 128;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = size;
  const ctx = canvas.getContext("2d");
  const gradient = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
  gradient.addColorStop(0, "rgba(0,0,0,0.55)");
  gradient.addColorStop(1, "rgba(0,0,0,0)");
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, size, size);
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

export async function createViewer(canvas, options = {}) {
  const reduced = Boolean(options.reduced);
  let dark = options.dark !== false;
  let palette = dark ? THEME.dark : THEME.light;

  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: true,
    alpha: true,
    powerPreference: "low-power",
  });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = dark ? 1.05 : 0.9;

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(35, 1, 0.1, 100);
  camera.position.set(0, 0.25, 9.2);
  camera.lookAt(0, 0.1, 0);

  // Image-based lighting from a procedural room, not a real HDRI: an .hdr file would
  // cost 300KB-1MB and hit the same asset-arrival problem the body model itself is
  // waiting on, to light a figure that (today, and even post-NOR-6) carries no baked
  // textures. The environment is generated once and the generator dropped immediately
  // — only the resulting texture is kept.
  const pmrem = new THREE.PMREMGenerator(renderer);
  const roomEnv = new RoomEnvironment();
  const envTexture = pmrem.fromScene(roomEnv, 0.04).texture;
  roomEnv.dispose();
  pmrem.dispose();
  scene.environment = envTexture;
  scene.environmentIntensity = 0.5;

  const key = new THREE.DirectionalLight(0xffffff, 1.5);
  key.position.set(3.5, 5, 6);
  scene.add(key);

  const rim = new THREE.DirectionalLight(0x8ec6ff, 0.7);
  rim.position.set(-4, 1.5, -5);
  scene.add(rim);

  const shadowTexture = createShadowTexture();
  const shadowMaterial = new THREE.MeshBasicMaterial({
    map: shadowTexture,
    transparent: true,
    depthWrite: false,
    toneMapped: false,
  });
  const shadowGeometry = new THREE.CircleGeometry(1.6, 32);
  const shadow = new THREE.Mesh(shadowGeometry, shadowMaterial);
  shadow.rotation.x = -Math.PI / 2;
  shadow.position.y = -2.15;
  // On scene, not group: group.rotation.y turns every frame (see tick()), and a
  // shadow spinning with the body it belongs to would read as a bug, not a feature.
  scene.add(shadow);

  const { group, regions, inert } = await loadFigure(palette);
  scene.add(group);

  // ---------------------------------------------------------------------
  // Interaction: a drag to rotate is the whole control surface, so pulling in
  // OrbitControls would double the download for one axis.
  // ---------------------------------------------------------------------
  let targetY = 0.35;
  let currentY = 0.35;
  let dragging = false;
  let lastX = 0;

  const onPointerDown = (e) => {
    dragging = true;
    lastX = e.clientX;
    canvas.setPointerCapture(e.pointerId);
  };
  const onPointerMove = (e) => {
    if (!dragging) return;
    targetY += (e.clientX - lastX) * 0.01;
    lastX = e.clientX;
  };
  const onPointerRelease = (e) => {
    dragging = false;
    if (e.pointerId !== undefined && canvas.hasPointerCapture(e.pointerId)) {
      canvas.releasePointerCapture(e.pointerId);
    }
  };
  canvas.addEventListener("pointerdown", onPointerDown);
  canvas.addEventListener("pointermove", onPointerMove);
  canvas.addEventListener("pointerup", onPointerRelease);
  canvas.addEventListener("pointercancel", onPointerRelease);

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
  let ember = readCSSColor("--north-ember", 0xe8973c);
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
      material.emissiveIntensity = 0.15 + 0.85 * intensity;
    }
  }

  return {
    setLoads,
    setTheme(isDark) {
      dark = isDark;
      palette = dark ? THEME.dark : THEME.light;
      renderer.toneMappingExposure = dark ? 1.05 : 0.9;
      base.set(palette.base);
      inert.color.set(palette.inert);
      ember = readCSSColor("--north-ember", 0xe8973c);
      setLoads(current);
    },
    destroy() {
      cancelAnimationFrame(frame);
      running = false;
      resizeObserver.disconnect();
      visibility.disconnect();
      canvas.removeEventListener("pointerdown", onPointerDown);
      canvas.removeEventListener("pointermove", onPointerMove);
      canvas.removeEventListener("pointerup", onPointerRelease);
      canvas.removeEventListener("pointercancel", onPointerRelease);

      const geometries = new Set();
      const materials = new Set();
      group.traverse((obj) => {
        if (!obj.isMesh) return;
        geometries.add(obj.geometry);
        materials.add(obj.material);
      });
      for (const geometry of geometries) geometry.dispose();
      for (const material of materials) material.dispose();

      shadowGeometry.dispose();
      shadowMaterial.dispose();
      shadowTexture.dispose();
      envTexture.dispose();

      scene.clear();
      renderer.dispose();
      renderer.forceContextLoss();
    },
  };
}

/**
 * Resolves the figure for the viewer. Today this just wraps buildFigure(); NOR-6's
 * follow-up replaces the body with a GLTFLoader fetch of a real anatomical model,
 * matching named meshes to the same muscle keys — the return shape doesn't change.
 */
async function loadFigure(palette) {
  return buildFigure(palette);
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
