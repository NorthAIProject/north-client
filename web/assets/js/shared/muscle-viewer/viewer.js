/**
 * The muscle viewer (NOR-8). Shared between the marketing landing page and the
 * real /app/training pages — one module, two callers, no page-specific logic here.
 *
 * The figure is a real anatomical model: a Z-Anatomy-derived muscle set
 * (CC BY-SA 4.0, hpfrei/body-anatomy-3d-viewer) sealed inside an opaque athletic
 * skin — see web/assets/models/README.md for the full attribution and how
 * body.glb was built. Every muscle mesh is tagged with a muscle key by name.
 *
 * NOR-6 changed what you see. The figure used to be a translucent shell with grey
 * muscles permanently visible underneath, which read as a blocky anatomy diagram.
 * Now the body is solid and lit like a product shot, and only the muscles an
 * exercise actually works light up — glowing out through the skin from inside
 * rather than being drawn on top of it. See glowMaterial() for how that works.
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
import { GLTFLoader } from "/assets/js/vendor/three-gltf-loader.module.js";
import { MeshoptDecoder } from "/assets/js/vendor/three-meshopt-decoder.module.js";
// Shared with tools/model/build-body.mjs, which uses the same table to decide what
// goes into body.glb in the first place — see muscles.js.
import { MUSCLE_ALIASES, MUSCLE_INFO, resolveKey } from "./muscles.js";

// The figure has to sit on the panel in both themes. Since NOR-6 the body carries
// its own baked colour, so a theme is only how brightly it's lit and what colour
// separates it from the card behind it — not the figure's own palette. The effort
// colour (ember) is a brand token, read live from CSS below.
const THEME = {
  dark: { exposure: 1.05, rim: 0x8ec6ff, rimStrength: 0.4 },
  light: { exposure: 0.85, rim: 0x2b3a52, rimStrength: 0.22 },
};

// Fallback skin shading for a model with no baked textures. body.glb is expected
// to ship UVs and PBR maps; if it doesn't (or a future rebuild drops them) the
// figure still renders as a solid body rather than falling back to the pre-NOR-6
// translucent shell.
const SKIN_FALLBACK = { color: 0xb98963, roughness: 0.68 };

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

// A studio softbox rig, built as geometry and baked into an environment map by
// PMREMGenerator. This is what an .hdr file would buy us, for no bytes: the
// ticket asked for HDRI lighting, but a real one costs 300KB-1MB on top of the
// model download. Three emissive panels (warm key, cool fill, overhead strip)
// plus a dim floor bounce is enough structure for skin to read as skin — the
// point is that reflections have *shape*, not that they depict a real room.
// Colours exceed 1.0 deliberately: PMREM renders to a half-float target, so the
// panels stay HDR and specular highlights keep their punch through ACES.
function createStudioEnvironment() {
  const env = new THREE.Scene();
  const geometry = new THREE.PlaneGeometry(1, 1);

  const panel = (hex, intensity, scale, position, lookAt) => {
    const material = new THREE.MeshBasicMaterial({ side: THREE.DoubleSide });
    material.color.setHex(hex).multiplyScalar(intensity);
    const mesh = new THREE.Mesh(geometry, material);
    mesh.scale.set(scale[0], scale[1], 1);
    mesh.position.set(position[0], position[1], position[2]);
    mesh.lookAt(lookAt[0], lookAt[1], lookAt[2]);
    env.add(mesh);
    return mesh;
  };

  panel(0xfff4e6, 7.5, [9, 12], [5, 5, 7], [0, 0, 0]); // key, warm, front-right
  panel(0xdce8ff, 2.6, [8, 10], [-6, 2, 3], [0, 0, 0]); // fill, cool, front-left
  panel(0xffffff, 4.0, [10, 4], [0, 9, -1], [0, 0, 0]); // overhead strip
  panel(0x6b6257, 0.9, [12, 12], [0, -6, 2], [0, 0, 0]); // floor bounce
  panel(0xa8c4e8, 1.4, [7, 10], [0, 1, -9], [0, 0, 0]); // back separation

  return env;
}

// The glow-through shader. A muscle lives *inside* an opaque body, so it can't be
// lit conventionally — nothing would ever see it. Instead each worked muscle is
// drawn after the skin with additive blending and depthFunc GreaterDepth, which
// means it only renders where the skin is already in front of it: the light reads
// as coming from under the surface.
//
// Two terms shape it into something volumetric rather than a flat decal:
//
//   fresnel  — glancing surfaces contribute most, so a muscle glows brightest at
//              its edges and where it wraps away from the camera.
//   depth    — GreaterDepth alone also passes for muscles on the *far* side of the
//              body (nothing but the skin writes depth, so the far quad is "behind
//              the skin" too, and the torso would light up from behind). uDepthMid
//              is the camera's distance to the figure's centre; anything further
//              than that fades out over uDepthFade, which is what keeps the body
//              reading as solid.
//
// Blending is alpha, not additive. Additive is the obvious choice for something
// called a glow and it's wrong here: the landing page card is white and the skin
// is a light tan, so adding light to it does nothing except at the few pixels
// where the body is already dark — the figure ends up looking like it's on fire
// along its silhouette and flat everywhere else. Compositing ember *over* the skin
// instead reads the same on a white card and on the dark /app shell.
const GLOW_VERTEX = /* glsl */ `
  varying vec3 vWorldNormal;
  varying vec3 vViewDir;
  varying float vCameraDist;
  void main() {
    vec4 worldPosition = modelMatrix * vec4(position, 1.0);
    vWorldNormal = normalize(mat3(modelMatrix) * normal);
    vViewDir = normalize(cameraPosition - worldPosition.xyz);
    vCameraDist = distance(cameraPosition, worldPosition.xyz);
    gl_Position = projectionMatrix * viewMatrix * worldPosition;
  }
`;

const GLOW_FRAGMENT = /* glsl */ `
  uniform vec3 uColor;
  uniform float uIntensity;
  uniform float uDepthMid;
  uniform float uDepthFade;
  varying vec3 vWorldNormal;
  varying vec3 vViewDir;
  varying float vCameraDist;
  void main() {
    float facing = abs(dot(normalize(vWorldNormal), normalize(vViewDir)));
    float fresnel = pow(1.0 - facing, 1.5);
    // Base term carries most of it and fresnel only adds shape. Leaning on fresnel
    // instead reads well in profile and then fades out exactly when the viewer turns
    // a muscle to face them — which is the moment they're trying to look at it.
    float body = 0.55 + 0.45 * fresnel;
    float depthMask = 1.0 - smoothstep(uDepthMid, uDepthMid + uDepthFade, vCameraDist);
    // Hotter towards the rim, so a muscle still has interior shape once the alpha
    // has flattened out near 1.
    vec3 tint = uColor * (0.9 + 0.45 * fresnel);
    gl_FragColor = vec4(tint, clamp(body * depthMask * uIntensity, 0.0, 1.0));
    #include <tonemapping_fragment>
    #include <colorspace_fragment>
  }
`;

function createGlowMaterial(color, depthMid) {
  return new THREE.ShaderMaterial({
    uniforms: {
      uColor: { value: color.clone() },
      uIntensity: { value: 0 },
      uDepthMid: { value: depthMid },
      uDepthFade: { value: 1.1 },
    },
    vertexShader: GLOW_VERTEX,
    fragmentShader: GLOW_FRAGMENT,
    transparent: true,
    depthWrite: false,
    depthFunc: THREE.GreaterDepth,
    side: THREE.FrontSide,
  });
}

// ?muscleDebug=1 makes the skin translucent so muscle geometry poking through it
// is obvious. Alignment between the two source meshes is the fragile part of the
// asset pipeline (see tools/model/README.md) and this is how you check it.
function isDebug() {
  try {
    return new URLSearchParams(location.search).get("muscleDebug") === "1";
  } catch {
    return false;
  }
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
  renderer.toneMappingExposure = palette.exposure;

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(35, 1, 0.1, 100);
  camera.position.set(0, 0.25, 9.2);
  camera.lookAt(0, 0.1, 0);

  // Distance from the camera to the figure's centre, which is what the glow shader
  // fades past to stop far-side muscles bleeding through the torso. Derived rather
  // than hardcoded so moving the camera above can't silently desync the two.
  const depthMid = camera.position.distanceTo(new THREE.Vector3(0, 0.1, 0));

  // Image-based lighting from a procedural studio rig baked through PMREM — see
  // createStudioEnvironment() for why this isn't an .hdr file. Generated once, and
  // both the source scene and the generator are dropped immediately; only the
  // resulting cube texture is kept.
  const pmrem = new THREE.PMREMGenerator(renderer);
  const studioEnv = createStudioEnvironment();
  const envTexture = pmrem.fromScene(studioEnv, 0.03).texture;
  studioEnv.traverse((obj) => {
    if (obj.isMesh) {
      obj.geometry.dispose();
      obj.material.dispose();
    }
  });
  pmrem.dispose();
  scene.environment = envTexture;
  scene.environmentIntensity = 1.0;

  // The environment does most of the work now that the body has real materials.
  // These two are for shaping only: a soft key to keep the chest and quads from
  // going flat, and a cool back rim so the silhouette separates from the card.
  const key = new THREE.DirectionalLight(0xfff1e0, 0.85);
  key.position.set(3.5, 5, 6);
  scene.add(key);

  const rim = new THREE.DirectionalLight(palette.rim, 0.55);
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

  let ember = readCSSColor("--north-ember", 0xe8973c);
  const { group, regions, skin } = await loadFigure(ember, depthMid, palette);
  scene.add(group);

  const muscleMeshes = Object.values(regions).flatMap((region) => region.meshes);

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
  //
  // Muscle meshes only, never the skin: since NOR-6 the skin is opaque and sits
  // in front of every muscle, so a raycast against the whole group would return
  // the shell for every single click. Three skips invisible objects, and a muscle
  // at zero load is invisible (see setLoads), so this also lands on exactly the
  // muscles that are currently glowing — clicking a dark region does nothing,
  // which matches what the person can actually see.
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

    for (const hit of raycaster.intersectObjects(muscleMeshes, false)) {
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
  let current = [];

  // A muscle at zero load isn't dimmed, it's switched off: it's sealed inside an
  // opaque body, so anything less than "off" would read as the skin being dirty.
  // Hiding it also drops it out of the draw call list and out of the raycast —
  // on a typical exercise fewer than a dozen of the 116 meshes are worked.
  function setLoads(loads) {
    current = loads;
    const peak = loads.reduce((m, l) => Math.max(m, l.share), 0) || 1;

    for (const region of Object.values(regions)) {
      region.material.uniforms.uIntensity.value = 0;
      for (const mesh of region.meshes) mesh.visible = false;
    }

    for (const load of loads) {
      const region = regions[load.key];
      if (!region) continue;
      const intensity = Math.min(1, load.share / peak);
      // Floor of 0.22 so a stabiliser still reads as lit rather than as a smudge;
      // the tier spread above it is what distinguishes primary from secondary. The
      // ceiling stays under 1 so even a primary mover keeps some skin reading over
      // it — at full opacity the muscle stops looking like it's under the surface.
      //
      // These numbers are per *layer*, not per muscle group. Glow meshes don't write
      // depth (they can't — see createGlowMaterial), so where a group is several
      // sheets deep the alphas composite. "abs" is the worst case at four layers
      // (transversus, both obliques, rectus) and turns into a flat slab across the
      // whole abdomen if a single layer is allowed to be strong. Anything much above
      // this trades a legible quad for an unreadable torso.
      region.material.uniforms.uIntensity.value = 0.32 + 0.42 * intensity;
      for (const mesh of region.meshes) mesh.visible = true;
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
      renderer.toneMappingExposure = palette.exposure;
      rim.color.set(palette.rim);
      skin.setRim(palette);
      ember = readCSSColor("--north-ember", 0xe8973c);
      for (const region of Object.values(regions)) {
        region.material.uniforms.uColor.value.copy(ember);
      }
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
      // Material.dispose() doesn't touch the textures the material points at, and
      // the skin's baked PBR maps are the only textures in the file that came from
      // the GLTF rather than being generated here.
      skin.disposeTextures();

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

function isUnderSkinNode(obj) {
  for (let n = obj; n; n = n.parent) {
    if (n.name === "skin") return true;
  }
  return false;
}

/**
 * Builds the skin's material from whatever the asset actually shipped.
 *
 * The intended input is a Tripo/Meshy export with UVs and baked PBR maps, in which
 * case its own material is kept (that's the whole reason for regenerating the
 * model) and only forced opaque. A model with no maps falls back to flat shading
 * so the viewer degrades to "plain body" rather than to a broken one.
 *
 * The fresnel rim is what stops a dark body dissolving into a dark card. It's
 * injected into the standard material rather than replacing it, so albedo,
 * roughness, normals and the environment map all keep working.
 */
function buildSkin(source, palette, debug) {
  const material =
    source && source.isMeshStandardMaterial
      ? source
      : new THREE.MeshStandardMaterial(SKIN_FALLBACK);

  material.metalness = 0;
  if (!material.map) {
    material.color.setHex(SKIN_FALLBACK.color);
    material.roughness = SKIN_FALLBACK.roughness;
  }

  // ?muscleDebug=1 only — production skin is opaque, which is what lets the glow
  // pass use GreaterDepth at all.
  material.transparent = debug;
  material.opacity = debug ? 0.25 : 1;
  material.depthWrite = !debug;

  const rimColor = { value: new THREE.Color(palette.rim) };
  const rimStrength = { value: palette.rimStrength };

  material.onBeforeCompile = (shader) => {
    shader.uniforms.uRimColor = rimColor;
    shader.uniforms.uRimStrength = rimStrength;
    shader.fragmentShader = shader.fragmentShader
      .replace(
        "#include <common>",
        "#include <common>\nuniform vec3 uRimColor;\nuniform float uRimStrength;",
      )
      .replace(
        "#include <opaque_fragment>",
        `{
          float rim = 1.0 - abs(dot(normalize(normal), normalize(vViewPosition)));
          outgoingLight += uRimColor * pow(rim, 2.5) * uRimStrength;
        }
        #include <opaque_fragment>`,
      );
  };
  material.needsUpdate = true;

  const textures = [
    material.map,
    material.normalMap,
    material.roughnessMap,
    material.metalnessMap,
    material.aoMap,
  ].filter(Boolean);

  return {
    material,
    setRim(next) {
      rimColor.value.set(next.rim);
      rimStrength.value = next.rimStrength;
    },
    disposeTextures() {
      for (const texture of textures) texture.dispose();
    },
  };
}

/**
 * Loads body.glb, walks the scene, and rebuilds every mesh's material in place.
 *
 * Muscle meshes share one glow material per key across both sides and every
 * anatomical head, so `setLoads` sets one uniform instead of walking the scene
 * graph; the mesh list beside it is what gets shown and hidden. The skin keeps
 * its own baked material. Anything unmatched is a leftover from the asset build
 * and is hidden outright — it's sealed inside an opaque body, so drawing it can
 * only cost frames.
 */
async function loadFigure(ember, depthMid, palette) {
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

  const debug = isDebug();
  const regions = {};

  function regionFor(key) {
    if (!regions[key]) {
      regions[key] = { material: createGlowMaterial(ember, depthMid), meshes: [] };
    }
    return regions[key];
  }

  const discardedMaterials = new Set();
  const unmatchedNames = [];
  const orphanedKeys = new Set(Object.keys(MUSCLE_ALIASES));
  let skinSource = null;

  inner.traverse((obj) => {
    if (!obj.isMesh) return;
    obj.frustumCulled = false;

    if (isUnderSkinNode(obj)) {
      skinSource = obj.material;
      obj.renderOrder = 0;
      return;
    }

    if (obj.material) discardedMaterials.add(obj.material);

    const key = resolveKey(obj.name) || resolveKey(obj.parent && obj.parent.name);
    if (key) {
      const region = regionFor(key);
      obj.material = region.material;
      // Drawn after the skin has written depth, which is what GreaterDepth in the
      // glow shader tests against.
      obj.renderOrder = 2;
      obj.visible = false;
      region.meshes.push(obj);
      orphanedKeys.delete(key);
    } else {
      obj.visible = false;
      unmatchedNames.push(obj.name);
    }
  });

  const skin = buildSkin(skinSource, palette, debug);
  inner.traverse((obj) => {
    if (obj.isMesh && isUnderSkinNode(obj)) obj.material = skin.material;
  });

  // The GLTF's own muscle materials are replaced above and never rendered — dispose
  // them rather than let them sit unused until GC. The skin's is deliberately not in
  // this set: buildSkin() keeps it for its baked maps.
  for (const material of discardedMaterials) material.dispose();

  if (!skinSource) {
    console.warn("[muscle-viewer] body.glb has no node tagged \"skin\" — see tools/model/README.md");
  }
  if (orphanedKeys.size > 0 || unmatchedNames.length > 0) {
    console.warn(
      "[muscle-viewer] asset naming drift — orphaned keys:",
      [...orphanedKeys],
      "unmatched meshes:",
      unmatchedNames,
    );
  }

  return { group, regions, skin };
}
