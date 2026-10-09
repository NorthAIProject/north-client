/**
 * The body figure (NOR-8). One module for every caller: My Day's body card, the
 * plan page's Muscles card, exercise pages and the landing demo.
 *
 * The figure is body-map.glb: the skin of a body cut into one region per muscle
 * key, built by scripts/bodymap from the anatomical atlas (attribution in
 * siteFooter(), web/landing/sections.templ). Each region is its own mesh and
 * material, named after its key, so colouring a muscle is setting one colour —
 * no geometry is reloaded — and a tap resolves to a key by the mesh's name.
 *
 * Untrained muscle is a cool grey-blue; trained muscle ramps from a pale amber
 * to ember. Skin with no muscle under it (head, hands, feet, joints) is a plain
 * neutral, so the muscle regions read as structure even with nothing trained.
 *
 * Ways to drive the colour, all ending in setHeat():
 *   - setHeat({quads: 0.8, ...})  — region key → 0..1. The body map: server
 *     computed (internal/lifts/lift.HeatOf, folded by internal/bodymap).
 *   - setMuscleGroups({primary, secondary, stabilizers}) — what an exercise
 *     works, at fixed tiers.
 *   - setLoads([{key, share}]) — the landing demo's percentages.
 *
 * Keys must already be region keys; folding (rhomboids → traps) is the
 * server's job, so this file never needs its own copy of the table.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";
import { GLTFLoader } from "/assets/js/vendor/three-gltf-loader.module.js";
import { MUSCLE_INFO } from "./muscles.js";
import { readCSSColor } from "../css-color.js";

const MODEL_PATH = "/assets/models/body-map.glb";
const BASE_REGION = "base";

// Fallbacks for the colour tokens in web/assets/css/input.css, used only when a
// page has not loaded the stylesheet.
const FALLBACK = {
  idle: 0x7d8ca1,
  base: 0x8b9097,
  heatLow: 0xf0d6a0,
  ember: 0xe8973c,
};

const THEME = {
  dark: { exposure: 0.95, rim: 0x8ec6ff, rimStrength: 0.3 },
  light: { exposure: 0.9, rim: 0x2b3a52, rimStrength: 0.15 },
};

// Muscles mostly seen from behind. When the hottest of these outweighs the
// front, the figure faces away by default, so a back day is not hidden.
const BACK_REGIONS = new Set(["traps", "lats", "erectors", "glutes", "hamstrings", "calves", "triceps", "neck"]);

const FRONT_YAW = 0.45; // front three-quarter, radians
const SWAY = 0.35; // idle sway either side of the facing, radians
const SWAY_PERIOD = 8; // seconds

// A soft radial-gradient disc under the figure. Cheaper than a shadow map, and
// the figure is always moving, so nothing about a real shadow could be cached.
function createShadowTexture() {
  const size = 128;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = size;
  const ctx = canvas.getContext("2d");
  const gradient = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
  gradient.addColorStop(0, "rgba(0,0,0,0.5)");
  gradient.addColorStop(1, "rgba(0,0,0,0)");
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, size, size);
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

// A studio softbox rig baked into an environment map by PMREMGenerator: what an
// .hdr file would buy, for no download. Colours exceed 1.0 deliberately — PMREM
// renders to a half-float target, so highlights keep their punch through ACES.
function createStudioEnvironment() {
  const env = new THREE.Scene();
  const geometry = new THREE.PlaneGeometry(1, 1);
  const panel = (hex, intensity, scale, position) => {
    const material = new THREE.MeshBasicMaterial({ side: THREE.DoubleSide });
    material.color.setHex(hex).multiplyScalar(intensity);
    const mesh = new THREE.Mesh(geometry, material);
    mesh.scale.set(scale[0], scale[1], 1);
    mesh.position.set(position[0], position[1], position[2]);
    mesh.lookAt(0, 0, 0);
    env.add(mesh);
  };
  panel(0xfff4e6, 6.5, [9, 12], [5, 5, 7]); // key, warm, front-right
  panel(0xdce8ff, 2.4, [8, 10], [-6, 2, 3]); // fill, cool, front-left
  panel(0xffffff, 3.5, [10, 4], [0, 9, -1]); // overhead strip
  panel(0x6b6257, 0.8, [12, 12], [0, -6, 2]); // floor bounce
  panel(0xa8c4e8, 1.4, [7, 10], [0, 1, -9]); // back separation
  return env;
}

// Cheap to check before spending the download and the WebGLRenderer
// constructor, which throws inconsistently across browsers. A thrown
// createViewer() means "no 3D here": callers show the flat figure.
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

function readColours() {
  return {
    idle: readCSSColor("--north-body-idle", FALLBACK.idle),
    base: readCSSColor("--north-body-base", FALLBACK.base),
    heatLow: readCSSColor("--north-heat-low", FALLBACK.heatLow),
    ember: readCSSColor("--north-ember", FALLBACK.ember),
  };
}

// A fresnel rim, injected into the standard material so roughness and the
// environment keep working. It is what stops a dark body dissolving into a
// dark card. The uniforms are shared, so a theme change is one assignment.
function addRim(material, rim) {
  material.onBeforeCompile = (shader) => {
    shader.uniforms.uRimColor = rim.color;
    shader.uniforms.uRimStrength = rim.strength;
    shader.fragmentShader = shader.fragmentShader
      .replace("#include <common>", "#include <common>\nuniform vec3 uRimColor;\nuniform float uRimStrength;")
      .replace(
        "#include <opaque_fragment>",
        `{
          float rim = 1.0 - abs(dot(normalize(normal), normalize(vViewPosition)));
          outgoingLight += uRimColor * pow(rim, 2.5) * uRimStrength;
        }
        #include <opaque_fragment>`,
      );
  };
  material.customProgramCacheKey = () => "north-body-rim";
}

export async function createViewer(canvas, options = {}) {
  if (!hasWebGL()) throw new Error("WebGL unavailable");

  const reduced = Boolean(options.reduced);
  let palette = options.dark !== false ? THEME.dark : THEME.light;
  let colours = readColours();

  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true, powerPreference: "low-power" });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = palette.exposure;

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(30, 1, 0.1, 50);

  const pmrem = new THREE.PMREMGenerator(renderer);
  const studio = createStudioEnvironment();
  const envTexture = pmrem.fromScene(studio, 0.03).texture;
  studio.traverse((obj) => {
    if (obj.isMesh) {
      obj.geometry.dispose();
      obj.material.dispose();
    }
  });
  pmrem.dispose();
  scene.environment = envTexture;
  // Enough to model the form; more washes the region colours out.
  scene.environmentIntensity = 0.65;

  // Shaping only; the environment does most of the lighting.
  const key = new THREE.DirectionalLight(0xfff1e0, 0.9);
  key.position.set(2, 3, 3);
  scene.add(key);
  const rimLight = new THREE.DirectionalLight(palette.rim, 0.5);
  rimLight.position.set(-2, 1, -3);
  scene.add(rimLight);

  const rim = {
    color: { value: new THREE.Color(palette.rim) },
    strength: { value: palette.rimStrength },
  };

  const { figure, regions, height } = await loadFigure(colours, rim);
  const group = new THREE.Group();
  group.add(figure);
  scene.add(group);

  const shadowTexture = createShadowTexture();
  const shadowMaterial = new THREE.MeshBasicMaterial({ map: shadowTexture, transparent: true, depthWrite: false, toneMapped: false });
  const shadowGeometry = new THREE.CircleGeometry(height * 0.3, 32);
  const shadow = new THREE.Mesh(shadowGeometry, shadowMaterial);
  shadow.rotation.x = -Math.PI / 2;
  shadow.position.y = 0.001;
  scene.add(shadow);

  // Frame the standing figure: aim at its middle from far enough back that
  // head and feet fit at any aspect the card gives us.
  function frame() {
    const fov = THREE.MathUtils.degToRad(camera.fov);
    const fitHeight = (height * 1.04) / 2 / Math.tan(fov / 2);
    const fitWidth = fitHeight / Math.min(1, camera.aspect * 1.6);
    camera.position.set(0, height * 0.52, Math.max(fitHeight, fitWidth));
    camera.lookAt(0, height * 0.5, 0);
  }

  // -------------------------------------------------------------------------
  // Interaction: drag turns the figure; a short, still press is a tap.
  // -------------------------------------------------------------------------
  let facing = FRONT_YAW;
  let dragOffset = 0;
  let dragging = false;
  let lastX = 0;
  let down = { x: 0, y: 0, at: 0 };
  let lastDragAt = -Infinity;

  const onPointerDown = (e) => {
    dragging = true;
    lastX = e.clientX;
    down = { x: e.clientX, y: e.clientY, at: performance.now() };
    canvas.setPointerCapture(e.pointerId);
  };
  const onPointerMove = (e) => {
    if (!dragging) return;
    dragOffset += (e.clientX - lastX) * 0.012;
    lastX = e.clientX;
    lastDragAt = performance.now();
  };
  const onPointerUp = (e) => {
    dragging = false;
    if (e.pointerId !== undefined && canvas.hasPointerCapture(e.pointerId)) {
      canvas.releasePointerCapture(e.pointerId);
    }
    const moved = Math.hypot(e.clientX - down.x, e.clientY - down.y);
    if (e.type === "pointerup" && moved <= 6 && performance.now() - down.at <= 500) tap(e);
  };
  canvas.addEventListener("pointerdown", onPointerDown);
  canvas.addEventListener("pointermove", onPointerMove);
  canvas.addEventListener("pointerup", onPointerUp);
  canvas.addEventListener("pointercancel", onPointerUp);

  const raycaster = new THREE.Raycaster();
  const pointer = new THREE.Vector2();
  const meshes = Object.values(regions).map((r) => r.mesh);
  let selected = null;

  function tap(e) {
    const rect = canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    pointer.x = ((e.clientX - rect.left) / rect.width) * 2 - 1;
    pointer.y = -((e.clientY - rect.top) / rect.height) * 2 + 1;
    raycaster.setFromCamera(pointer, camera);
    const hit = raycaster.intersectObjects(meshes, false)[0];
    const key = hit && hit.object.name !== BASE_REGION ? hit.object.name : null;
    select(key);
    if (options.onMuscleClick) {
      options.onMuscleClick(key, key ? MUSCLE_INFO[key] || null : null, {
        x: (e.clientX - rect.left) / rect.width,
        y: (e.clientY - rect.top) / rect.height,
      });
    }
  }

  function select(key) {
    if (selected && regions[selected]) regions[selected].material.emissive.setScalar(0);
    selected = key;
    if (selected && regions[selected]) regions[selected].material.emissive.setScalar(0.05);
  }

  // -------------------------------------------------------------------------
  // Sizing, and a render loop paused whenever the canvas is off screen
  // -------------------------------------------------------------------------
  function resize() {
    const width = canvas.clientWidth;
    const height = canvas.clientHeight;
    if (!width || !height) return;
    renderer.setSize(width, height, false);
    camera.aspect = width / height;
    camera.updateProjectionMatrix();
    frame();
  }
  const resizeObserver = new ResizeObserver(resize);
  resizeObserver.observe(canvas);
  resize();

  let running = true;
  let raf = 0;
  const start = performance.now();
  let yaw = facing;

  const visibility = new IntersectionObserver((entries) => {
    const visible = entries.some((e) => e.isIntersecting);
    if (visible && !running) {
      running = true;
      raf = requestAnimationFrame(tick);
    } else if (!visible) {
      running = false;
    }
  });
  visibility.observe(canvas);

  function tick(now) {
    if (!running) return;
    // A slow sway around the facing, paused for a few seconds after a drag so
    // the figure stays where the person put it; none at all for reduced motion.
    const idle = !reduced && !dragging && now - lastDragAt > 4000;
    const sway = idle ? Math.sin(((now - start) / 1000 / SWAY_PERIOD) * Math.PI * 2) * SWAY : 0;
    const target = facing + dragOffset + sway;
    yaw += (target - yaw) * (reduced ? 1 : 0.08);
    group.rotation.y = yaw;
    renderer.render(scene, camera);
    raf = requestAnimationFrame(tick);
  }
  raf = requestAnimationFrame(tick);

  // -------------------------------------------------------------------------
  // Colour
  // -------------------------------------------------------------------------
  let heat = {};
  const scratch = new THREE.Color();

  function colourFor(key, intensity) {
    if (key === BASE_REGION) return colours.base;
    if (!(intensity > 0)) return colours.idle;
    // Even a light touch reads as warm: start a quarter of the way up the ramp.
    const t = 0.25 + 0.75 * Math.min(1, intensity);
    return scratch.copy(colours.heatLow).lerp(colours.ember, t);
  }

  function setHeat(next = {}) {
    heat = { ...next };
    for (const [key, region] of Object.entries(regions)) {
      region.material.color.copy(colourFor(key, heat[key] || 0));
    }
    let front = 0;
    let back = 0;
    for (const [key, value] of Object.entries(heat)) {
      if (BACK_REGIONS.has(key)) back = Math.max(back, value);
      else front = Math.max(front, value);
    }
    facing = back > front ? Math.PI + FRONT_YAW : FRONT_YAW;
  }

  function setMuscleGroups({ primary = [], secondary = [], stabilizers = [] } = {}) {
    const next = {};
    const put = (keys, value) => {
      for (const k of keys) next[k] = Math.max(next[k] || 0, value);
    };
    put(stabilizers, 0.25);
    put(secondary, 0.55);
    put(primary, 1);
    setHeat(next);
  }

  function setLoads(loads = []) {
    const peak = loads.reduce((m, l) => Math.max(m, l.share), 0) || 1;
    const next = {};
    for (const load of loads) next[load.key] = Math.max(next[load.key] || 0, load.share / peak);
    setHeat(next);
  }

  setHeat({});

  return {
    setHeat,
    setMuscleGroups,
    setLoads,
    select,
    // Turn to show the front or the back, for a caller with its own toggle.
    face(side) {
      facing = side === "back" ? Math.PI + FRONT_YAW : FRONT_YAW;
      dragOffset = 0;
    },
    setTheme(isDark) {
      palette = isDark ? THEME.dark : THEME.light;
      renderer.toneMappingExposure = palette.exposure;
      rimLight.color.set(palette.rim);
      rim.color.value.set(palette.rim);
      rim.strength.value = palette.rimStrength;
      colours = readColours();
      setHeat(heat);
    },
    destroy() {
      cancelAnimationFrame(raf);
      running = false;
      resizeObserver.disconnect();
      visibility.disconnect();
      canvas.removeEventListener("pointerdown", onPointerDown);
      canvas.removeEventListener("pointermove", onPointerMove);
      canvas.removeEventListener("pointerup", onPointerUp);
      canvas.removeEventListener("pointercancel", onPointerUp);
      for (const region of Object.values(regions)) {
        region.mesh.geometry.dispose();
        region.material.dispose();
      }
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
 * Loads body-map.glb and gives every region its own material, keyed by the
 * mesh's name. The file stands on the origin (soles at y = 0, centred), so
 * nothing needs measuring beyond its height.
 */
async function loadFigure(colours, rim) {
  // Versioned like the module itself: /assets/ is served immutable for a year.
  const version = new URL(import.meta.url).searchParams.get("v");
  const url = version ? `${MODEL_PATH}?v=${version}` : MODEL_PATH;
  const gltf = await new GLTFLoader().loadAsync(url);
  const figure = gltf.scene;

  const regions = {};
  figure.traverse((obj) => {
    if (!obj.isMesh) return;
    if (obj.material) obj.material.dispose();
    const material = new THREE.MeshStandardMaterial({
      color: obj.name === BASE_REGION ? colours.base : colours.idle,
      roughness: 0.62,
      metalness: 0,
      emissive: 0x000000,
    });
    addRim(material, rim);
    obj.material = material;
    regions[obj.name] = { mesh: obj, material };
  });
  const box = new THREE.Box3().setFromObject(figure);
  return { figure, regions, height: box.max.y - box.min.y };
}
