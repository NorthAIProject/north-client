/**
 * The muscle viewer (NOR-8). Shared between the marketing landing page and the
 * real /app/training pages — one module, two callers, no page-specific logic here.
 *
 * The figure is a real anatomical model (NOR-6): a Z-Anatomy-derived muscle set
 * (CC BY-SA 4.0, hpfrei/body-anatomy-3d-viewer) layered under a translucent skin
 * shell (CC BY, ferrumiron6) — see web/assets/models/README.md for the full
 * attribution and how body.glb was built. Every muscle mesh is tagged with a
 * muscle key by name.
 *
 * Two ways to drive the colouring:
 *   - setLoads([{key, share, role}]) — continuous, per-muscle percentages. Used
 *     by the landing demo, which has its own richer readout beside the canvas.
 *   - setMuscleGroups({primary, secondary, stabilizers}) — the production data
 *     contract (NOR-8): three flat arrays of muscle keys, no percentages, because
 *     that's what the AI plan generator actually produces. This is an adapter
 *     over setLoads with a fixed intensity per tier, not a separate code path.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";
import { RoomEnvironment } from "/assets/js/vendor/three-room-environment.module.js";
import { GLTFLoader } from "/assets/js/vendor/three-gltf-loader.module.js";
import { MeshoptDecoder } from "/assets/js/vendor/three-meshopt-decoder.module.js";

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

// Cheap to check before spending the GLTF download and the WebGLRenderer
// constructor call (which throws, inconsistently across browsers, rather than
// failing predictably) — callers treat a thrown createViewer() as "no 3D here,
// show the fallback" (see the alpine wrapper's load()).
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

export async function createViewer(canvas, options = {}) {
  if (!hasWebGL()) throw new Error("WebGL unavailable");

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
  let downX = 0;
  let downY = 0;
  let downAt = 0;

  const onPointerDown = (e) => {
    dragging = true;
    lastX = e.clientX;
    downX = e.clientX;
    downY = e.clientY;
    downAt = performance.now();
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
  // Click-to-inspect: a pointerup that barely moved and didn't linger is a
  // click, not the end of a drag-to-rotate. Raycast against the figure and
  // resolve the hit mesh back to a muscle key the same way loadFigure() does,
  // so "what did I click" and "what does setLoads colour" never disagree.
  // ---------------------------------------------------------------------
  const raycaster = new THREE.Raycaster();
  const pointerNDC = new THREE.Vector2();

  const onPointerClick = (e) => {
    if (!options.onMuscleClick) return;
    const moved = Math.hypot(e.clientX - downX, e.clientY - downY);
    if (moved > 6 || performance.now() - downAt > 500) return;

    const rect = canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    pointerNDC.x = ((e.clientX - rect.left) / rect.width) * 2 - 1;
    pointerNDC.y = -((e.clientY - rect.top) / rect.height) * 2 + 1;
    raycaster.setFromCamera(pointerNDC, camera);

    for (const hit of raycaster.intersectObject(group, true)) {
      const key = resolveKey(hit.object.name) || resolveKey(hit.object.parent && hit.object.parent.name);
      if (key) {
        options.onMuscleClick(key, MUSCLE_INFO[key] || null);
        return;
      }
    }
  };
  canvas.addEventListener("pointerup", onPointerClick);

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

  // Production data contract (NOR-8): three flat key arrays, no percentages —
  // that's what the AI plan generator returns. share values here are fixed
  // per tier, chosen to match the opacity tiers the landing readout already
  // uses for Primary/Secondary/Stabiliser (demos.templ's roleShade), so the
  // two call sites stay visually consistent even though only one of them
  // exposes numbers to the person looking at it.
  function setMuscleGroups({ primary = [], secondary = [], stabilizers = [] } = {}) {
    setLoads([
      ...primary.map((key) => ({ key, share: 100, role: "primary" })),
      ...secondary.map((key) => ({ key, share: 55, role: "secondary" })),
      ...stabilizers.map((key) => ({ key, share: 25, role: "stabilizer" })),
    ]);
  }

  return {
    setLoads,
    setMuscleGroups,
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
      canvas.removeEventListener("pointerup", onPointerClick);

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

const MODEL_PATH = "/assets/models/body.glb";

// Exact node names from body.glb, one entry per muscle key. Both sides and every
// anatomical head/segment share a key — that's how a limb pair lights up together.
// The short common-name aliases are a safety net for a future re-export that might
// not match these exact Z-Anatomy labels.
const MUSCLE_ALIASES = {
  quads: [
    "rectus femoris muscle",
    "vastus lateralis muscle",
    "vastus medialis muscle",
    "vastus intermedius muscle",
    "quadriceps",
  ],
  glutes: [
    "gluteus medius muscle",
    "gluteus maximus muscle",
    "gluteus minimus muscle",
    "glutes",
  ],
  hamstrings: [
    "long head of biceps femoris",
    "short head of biceps femoris",
    "semimembranosus muscle",
    "semitendinosus muscle",
    "hamstrings",
  ],
  calves: [
    "lateral head of gastrocnemius",
    "medial head of gastrocnemius",
    "soleus muscle",
    "calves",
  ],
  adductors: ["adductor magnus", "adductor longus", "adductor brevis", "adductors"],
  traps: [
    "ascending part of trapezius muscle",
    "descending part of trapezius muscle",
    "transverse part of trapezius muscle",
    "trapezius",
    "traps",
  ],
  delts: [
    "acromial part of deltoid muscle",
    "clavicular part of deltoid muscle",
    "scapular spinal part of deltoid muscle",
    "deltoid",
    "delts",
  ],
  biceps: ["long head of biceps brachii", "short head of biceps brachii", "biceps brachii", "biceps"],
  triceps: [
    "medial head of triceps brachii",
    "lateral head of triceps brachii",
    "long head of triceps brachii",
    "triceps brachii",
    "triceps",
  ],
  forearms: [
    "brachioradialis muscle",
    "flexor carpi radialis",
    "superficial head of pronator teres",
    "deep head of pronator teres",
    "humeral head of flexor carpi ulnaris",
    "ulnar head of flexor carpi ulnaris",
    "pronator quadratus",
    "ulnar head of extensor carpi ulnaris",
    "humeral head of extensor carpi ulnaris",
    "extensor carpi radialis longus",
    "extensor carpi radialis brevis",
    "forearms",
  ],
  lats: ["latissimus dorsi muscle", "latissimus dorsi", "lats"],
  rhomboids: ["rhomboid major muscle", "rhomboid minor muscle", "rhomboids"],
  // Thoracolumbar only. The capitis/colli heads of the same three columns
  // moved to `neck` below — ALIAS_LOOKUP is a Map built in key order, so a
  // mesh listed twice silently belongs to whichever key is written last.
  // Splitting them is also the more accurate read: what a deadlift loads is
  // the thoracolumbar erectors, not the neck extensors.
  erectors: [
    "iliocostalis lumborum muscle",
    "iliocostalis thoracis muscle",
    "longissimus thoracis muscle",
    "spinalis thoracis muscle",
    "semispinalis thoracis muscle",
    "erector spinae",
    "erectors",
  ],
  serratus: ["serratus anterior muscle", "serratus anterior", "serratus"],
  abs: [
    "rectus abdominis muscle",
    "external abdominal oblique muscle",
    "internal abdominal oblique muscle",
    "transversus abdominis muscle",
    "abdominals",
    "abs",
  ],
  // Deliberately empty: body.glb has no pectoralis mesh, so setLoads finds
  // nothing to colour and skips the key. The group is still canonical — see
  // UnmodelledGroups in internal/workouts/plan/muscle.go, and the note the
  // viewer component renders so a bench press does not appear to train
  // nothing. Fill this in when the model gains pec major/minor.
  chest: [],
  // The cervical heads of the erector columns, moved out of `erectors`
  // above. Not a sternocleidomastoid — body.glb has none — so this
  // highlights neck extension, which is what the neck-trained exercises in
  // the catalog actually are.
  neck: [
    "iliocostalis colli muscle",
    "longissimus capitis muscle",
    "longissimus colli muscle",
    "spinalis capitis muscle",
    "spinalis colli muscle",
    "semispinalis colli muscle",
    "neck",
  ],
};

// Display name + one-line description per muscle key, for the click-to-inspect
// panel. Same 17 keys as MUSCLE_ALIASES and internal/workouts/plan/muscle.go —
// see that file's doc comment for the checklist to keep all three in sync.
// A key whose MUSCLE_ALIASES entry is empty still belongs here: the model
// cannot colour it, but the person clicking around still deserves to be told
// what it is.
const MUSCLE_INFO = {
  quads: { name: "Quadriceps", description: "Front of the thigh; straightens the knee and drives out of a squat." },
  glutes: { name: "Glutes", description: "The hip's main extensor — powers standing up from a squat or hinge." },
  hamstrings: { name: "Hamstrings", description: "Back of the thigh; bends the knee and extends the hip in a hinge." },
  calves: { name: "Calves", description: "Back of the lower leg; points the foot, drives a calf raise or a jump." },
  adductors: { name: "Adductors", description: "Inner thigh; pulls the leg toward the midline and stabilises the squat." },
  traps: { name: "Trapezius", description: "Upper back and neck; shrugs the shoulders and stabilises overhead loads." },
  delts: { name: "Deltoids", description: "Cap of the shoulder; raises the arm out to the side or overhead." },
  biceps: { name: "Biceps", description: "Front of the upper arm; bends the elbow, as in a curl or a pull-up." },
  triceps: { name: "Triceps", description: "Back of the upper arm; straightens the elbow, as in a press." },
  forearms: { name: "Forearms", description: "Grip and wrist control; the limiting factor in most pulling work." },
  lats: { name: "Latissimus dorsi", description: "Broadest back muscle; pulls the arm down and in, as in a pull-up or row." },
  rhomboids: { name: "Rhomboids", description: "Between the shoulder blades; pulls them together, as in a row." },
  erectors: { name: "Erector spinae", description: "Runs the length of the spine; keeps the back straight under load." },
  serratus: { name: "Serratus anterior", description: "Side of the ribcage; rotates the shoulder blade during an overhead press." },
  abs: { name: "Abdominals", description: "Front of the core; braces the trunk against load, more than it bends it." },
  chest: { name: "Pectorals", description: "Front of the ribcage; pushes the arm forward and across, as in a bench press." },
  neck: { name: "Neck extensors", description: "Back of the neck; holds the head steady under load and extends it." },
};

// Built once at module scope: normalized alias text -> muscle key. Both the aliases
// here and every incoming mesh name go through the same normalizeName(), so this
// table doesn't need to anticipate GLTFLoader's node-name sanitization itself.
const ALIAS_LOOKUP = new Map();
for (const [key, aliases] of Object.entries(MUSCLE_ALIASES)) {
  for (const alias of aliases) ALIAS_LOOKUP.set(normalizeName(alias), key);
}

// GLTFLoader runs every node name through PropertyBinding.sanitizeNodeName() (so
// glTF names are safe as animation track target paths): spaces become underscores
// and periods are stripped outright, so "Iliocostalis lumborum muscle.001" arrives
// here as "Iliocostalis_lumborum_muscle001" — the duplicate-suffix digits end up
// glued straight onto the word with no separator at all. Unifying separators to
// spaces before stripping trailing digits makes both forms converge on the same
// normalized string regardless of which one a given alias or mesh name started as.
function normalizeName(name) {
  return (name || "")
    .toLowerCase()
    .replace(/[_.]+/g, " ")
    .replace(/\d+$/, "")
    .replace(/\s+/g, " ")
    .trim();
}

function resolveKey(name) {
  const normalized = normalizeName(name);
  if (!normalized) return null;
  if (ALIAS_LOOKUP.has(normalized)) return ALIAS_LOOKUP.get(normalized);
  // Fallback: substring containment, for names this exact table doesn't cover.
  for (const [alias, key] of ALIAS_LOOKUP) {
    if (normalized.includes(alias) || alias.includes(normalized)) return key;
  }
  return null;
}

function isUnderSkinNode(obj) {
  for (let n = obj; n; n = n.parent) {
    if (n.name === "skin") return true;
  }
  return false;
}

/**
 * Loads body.glb, walks the scene, and recolors every mesh in place: muscle meshes
 * get a per-key material (shared across both sides and every anatomical head, so
 * `setLoads` never has to walk the scene graph), the skin shell gets a fixed
 * translucent material, and anything unmatched falls through to `inert`.
 */
async function loadFigure(palette) {
  const loader = new GLTFLoader();
  loader.setMeshoptDecoder(MeshoptDecoder);

  // MODEL_PATH is versioned the same way the module itself is (landing.js appends
  // ?v=<deploy token> to the import), since mountAssets serves everything under
  // /assets/ with a one-year immutable Cache-Control.
  const moduleVersion = new URL(import.meta.url).searchParams.get("v");
  const url = moduleVersion ? `${MODEL_PATH}?v=${moduleVersion}` : MODEL_PATH;

  const gltf = await loader.loadAsync(url);
  const inner = gltf.scene;

  // Auto-frame: scale to the height this viewer's camera/lighting/contact-shadow
  // are tuned for, and recenter into a pivot group so rotation.y (driven every
  // frame by tick()) turns around the figure's own center rather than wherever
  // the source asset happened to put its origin.
  const box = new THREE.Box3().setFromObject(inner);
  const size = box.getSize(new THREE.Vector3());
  const TARGET_HEIGHT = 4.7;
  inner.scale.setScalar(TARGET_HEIGHT / size.y);

  const scaledBox = new THREE.Box3().setFromObject(inner);
  const center = scaledBox.getCenter(new THREE.Vector3());
  inner.position.x -= center.x;
  inner.position.z -= center.z;
  inner.position.y -= scaledBox.min.y + 2.15; // feet rest on the contact-shadow plane

  const group = new THREE.Group();
  group.add(inner);

  const regions = {};
  const inert = new THREE.MeshStandardMaterial({
    color: palette.inert,
    roughness: 0.72,
    metalness: 0,
  });
  const skinMaterial = new THREE.MeshStandardMaterial({
    color: 0xcaa27a,
    roughness: 0.55,
    metalness: 0,
    transparent: true,
    opacity: 0.55,
    depthWrite: false,
  });

  function materialFor(key) {
    if (!regions[key]) {
      regions[key] = new THREE.MeshStandardMaterial({
        color: palette.base,
        emissive: palette.base,
        emissiveIntensity: 0,
        roughness: 0.45,
        metalness: 0,
      });
    }
    return regions[key];
  }

  const originalMaterials = new Set();
  const unmatchedNames = [];
  const orphanedKeys = new Set(Object.keys(MUSCLE_ALIASES));

  inner.traverse((obj) => {
    if (!obj.isMesh) return;
    obj.frustumCulled = false;
    if (obj.material) originalMaterials.add(obj.material);

    if (isUnderSkinNode(obj)) {
      obj.material = skinMaterial;
      return;
    }

    const key = resolveKey(obj.name) || resolveKey(obj.parent && obj.parent.name);
    if (key) {
      obj.material = materialFor(key);
      orphanedKeys.delete(key);
    } else {
      obj.material = inert;
      unmatchedNames.push(obj.name);
    }
  });

  // The GLTF's own materials are replaced above and never rendered — dispose them
  // rather than let them sit unused until GC.
  for (const material of originalMaterials) material.dispose();

  if (orphanedKeys.size > 0 || unmatchedNames.length > 0) {
    console.warn(
      "[muscle-viewer] asset naming drift — orphaned keys:",
      [...orphanedKeys],
      "unmatched meshes:",
      unmatchedNames,
    );
  }

  return { group, regions, inert };
}
