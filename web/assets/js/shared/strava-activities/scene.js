/**
 * The calendar terrain.
 *
 * Owns the renderer, the lights, the resources every week shares, and the
 * residency window that decides which weeks currently exist on the GPU.
 *
 * Two rules hold this together, and both were learned from the scene it
 * replaces:
 *
 * Shared resources are disposed once, in destroy(). The box geometry, the
 * column material and the per-family line materials are used by every week;
 * the old viewer kept one flat list of disposables, which was correct only
 * because nothing was ever removed from that scene. Here weeks come and go,
 * and disposing a shared material with the first eviction would leave every
 * later week rendering black.
 *
 * The data outlives the geometry. Every page ever fetched stays in memory as
 * JSON — a few kilobytes a week — while the GPU objects for weeks outside the
 * residency window are dropped. Travelling back toward the present is then
 * free and re-requests nothing.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

import { readPalette } from "./palette.js";
import { buildWeek, WEEK_PITCH, COLUMN_SIZE } from "./terrain.js";
import { createCameraRig } from "./camera.js";
import { createLabels } from "./labels.js";

// How much terrain exists on the GPU at once, measured from the camera.
// Deliberately asymmetric and not a FIFO: travel back forty weeks and a
// queue would either evict everything behind you or hold all forty.
//
// KEEP_BEHIND is small because those weeks sit between the camera and what it
// is looking at, close enough to be cut by the bottom of the frame. KEEP_AHEAD
// covers the weeks the rig now frames — the fog, not an empty edge, is what
// ends the landscape.
const KEEP_BEHIND = 2;
const KEEP_AHEAD = 24;

// Ask for more weeks this far before running out of them.
const LOAD_AHEAD = 3;

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

export async function createScene(canvas, options) {
  if (!hasWebGL()) throw new Error("WebGL unavailable");

  const { reduced = false, onSelect = () => {}, onHover = () => {}, onNeedOlder = () => {} } = options;

  const palette = readPalette();

  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
  // Capped rather than left at devicePixelRatio: a standard material with a
  // shadow map at 3x on a phone-class GPU is a slideshow.
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 1.75));
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.shadowMap.enabled = true;
  renderer.shadowMap.type = THREE.PCFSoftShadowMap;

  const scene = new THREE.Scene();
  // Fog on the page colour, so weeks at the edge of the residency window
  // dissolve into the background instead of popping out of existence. This is
  // what makes disposal invisible.
  //
  // The range is set in resize(), from the distance the rig has settled on. It
  // was fixed at 12–34 against a camera that stood at 9.5; a camera that backs
  // off to frame a short canvas would otherwise start the haze in front of the
  // ground it is aimed at.
  scene.fog = new THREE.Fog(palette.background, 12, 34);

  scene.add(new THREE.HemisphereLight(0xffffff, 0x202830, 0.55));

  const key = new THREE.DirectionalLight(0xffffff, 1.15);
  key.position.set(-6, 12, 6);
  key.castShadow = true;
  key.shadow.mapSize.set(window.devicePixelRatio > 1.5 ? 512 : 1024, window.devicePixelRatio > 1.5 ? 512 : 1024);
  // Sized to the visible window, not the whole landscape. A shadow camera
  // covering twenty weeks gives about two texels per column.
  key.shadow.camera.left = -7;
  key.shadow.camera.right = 7;
  key.shadow.camera.top = 7;
  key.shadow.camera.bottom = -7;
  key.shadow.camera.near = 0.5;
  key.shadow.camera.far = 40;
  scene.add(key);
  scene.add(key.target);

  const fill = new THREE.DirectionalLight(0xffffff, 0.25);
  fill.position.set(5, 4, -8);
  scene.add(fill);

  // Shared by every week, disposed once.
  const columnGeometry = new THREE.BoxGeometry(1, 1, 1);
  const columnMaterial = new THREE.MeshStandardMaterial({ roughness: 0.75, metalness: 0 });
  const lineMaterials = {};

  const world = new THREE.Group();
  scene.add(world);

  // Selection is drawn as its own outline rather than by recolouring the
  // instance. Rewriting a week's colour buffer to highlight one day is the
  // churn pattern the landing world already warns about.
  const outline = new THREE.Mesh(
    new THREE.BoxGeometry(1, 1, 1),
    new THREE.MeshBasicMaterial({ color: palette.sport.other, wireframe: true, transparent: true, opacity: 0.9 }),
  );
  outline.visible = false;
  scene.add(outline);

  let weeksData = options.weeks.slice(); // oldest first
  const loadScale = options.loadScale || 0;
  const resident = new Map(); // offset -> built week
  let selectedDate = null;

  // offsetOf maps a week onto the travel axis: 0 is the newest week ever
  // seen, and older weeks count upward. Prepending older pages must not move
  // anything already drawn, so the newest week is the fixed origin.
  function offsetOf(index) {
    return weeksData.length - 1 - index;
  }

  function ensureResident(cameraWeek) {
    const from = Math.max(0, Math.floor(cameraWeek) - KEEP_BEHIND);
    const to = Math.floor(cameraWeek) + KEEP_AHEAD;

    for (let index = 0; index < weeksData.length; index++) {
      const offset = offsetOf(index);
      if (offset < from || offset > to) continue;
      if (resident.has(offset)) continue;

      const built = buildWeek(weeksData[index], offset, {
        palette,
        geometry: columnGeometry,
        material: columnMaterial,
        lineMaterials,
        loadScale,
      });
      world.add(built.group);
      resident.set(offset, built);
    }

    for (const [offset, built] of resident) {
      if (offset >= from && offset <= to) continue;
      world.remove(built.group);
      built.dispose();
      resident.delete(offset);
    }
  }

  function dayAt(offset, index) {
    const built = resident.get(offset);
    if (!built) return null;
    return built.days[index] || null;
  }

  function findDay(date) {
    for (let index = 0; index < weeksData.length; index++) {
      const week = weeksData[index];
      const dayIndex = week.days.findIndex((d) => d.date === date);
      if (dayIndex >= 0) return { offset: offsetOf(index), index: dayIndex };
    }
    return null;
  }

  function showOutline(date) {
    selectedDate = date;
    const found = date ? findDay(date) : null;
    if (!found) {
      outline.visible = false;
      return;
    }

    ensureResident(found.offset);
    const day = dayAt(found.offset, found.index);
    if (!day) {
      outline.visible = false;
      return;
    }

    outline.scale.set(COLUMN_SIZE * 1.06, day.height * 1.02, COLUMN_SIZE * 1.06);
    outline.position.set(day.x, day.height / 2, -found.offset * WEEK_PITCH);
    outline.visible = true;
  }

  // -------------------------------------------------------------------
  const raycaster = new THREE.Raycaster();
  const pointer = new THREE.Vector2();

  function pick(clientX, clientY) {
    const rect = canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return null;

    pointer.x = ((clientX - rect.left) / rect.width) * 2 - 1;
    pointer.y = -((clientY - rect.top) / rect.height) * 2 + 1;
    raycaster.setFromCamera(pointer, rig.camera);

    const meshes = [];
    for (const built of resident.values()) meshes.push(built.columns);

    const hit = raycaster.intersectObjects(meshes, false)[0];
    if (!hit || hit.instanceId === undefined) return null;

    for (const [offset, built] of resident) {
      if (built.columns !== hit.object) continue;
      const day = built.days[hit.instanceId];
      return day ? { offset, day, screen: { x: clientX - rect.left, y: clientY - rect.top } } : null;
    }
    return null;
  }

  const rig = createCameraRig(canvas, {
    reduced,
    onTravel: (week) => {
      ensureResident(week);
      if (week + LOAD_AHEAD > weeksData.length - 1) onNeedOlder();
      needsRender = true;
    },
    onClick: (e) => {
      const found = pick(e.clientX, e.clientY);
      const date = found ? found.day.day.date : null;
      showOutline(date);
      onSelect(date);
      needsRender = true;
    },
    onHoverMove: (e) => {
      const found = pick(e.clientX, e.clientY);
      onHover(found ? found.day.day : null, found ? found.screen : null);
    },
  });

  const labels = createLabels(options.labels);

  ensureResident(0);
  rig.setRange(weeksData.length);

  // -------------------------------------------------------------------
  let needsRender = true;

  function resize() {
    const width = canvas.clientWidth;
    const height = canvas.clientHeight;
    if (!width || !height) return;
    renderer.setSize(width, height, false);
    rig.resize(width, height);

    const reach = rig.distanceToTarget();
    scene.fog.near = reach + 2;
    scene.fog.far = reach + KEEP_AHEAD * 0.9;

    needsRender = true;
  }

  const resizeObserver = new ResizeObserver(resize);
  resizeObserver.observe(canvas);
  resize();

  let running = true;
  let frame = 0;

  // Two gates, not one. A full-bleed canvas is almost always intersecting, so
  // the intersection check alone never fires and the loop runs forever in a
  // background tab — the landing world's README records the same lesson.
  const visibility = new IntersectionObserver((entries) => {
    running = entries.some((e) => e.isIntersecting) && !document.hidden;
    if (running) frame = requestAnimationFrame(tick);
  });
  visibility.observe(canvas);

  const onVisibilityChange = () => {
    running = !document.hidden;
    if (running) frame = requestAnimationFrame(tick);
  };
  document.addEventListener("visibilitychange", onVisibilityChange);

  const onContextLost = (e) => {
    e.preventDefault();
    running = false;
  };
  canvas.addEventListener("webglcontextlost", onContextLost);

  function tick() {
    if (!running) return;

    const moved = rig.update();
    if (moved || needsRender) {
      // The shadow camera follows the target so its texels stay on the part
      // of the landscape anyone can see.
      key.target.position.set(0, 0, -rig.at());
      key.position.set(-6, 12, 6 - rig.at());

      labels.update(rig.camera, weeksData, rig.at(), canvas.clientWidth, canvas.clientHeight);
      renderer.render(scene, rig.camera);
      needsRender = false;
    }

    frame = requestAnimationFrame(tick);
  }
  frame = requestAnimationFrame(tick);

  return {
    select(date) {
      showOutline(date);
      const found = date ? findDay(date) : null;
      if (found) rig.travelTo(found.offset);
      needsRender = true;
    },

    // prepend takes an older page. The newest week stays the origin, so
    // nothing already on screen moves when the ground extends behind it.
    prepend(olderWeeks) {
      if (!olderWeeks || olderWeeks.length === 0) return;
      weeksData = olderWeeks.concat(weeksData);
      rig.setRange(weeksData.length);
      ensureResident(rig.at());
      needsRender = true;
    },

    destroy() {
      cancelAnimationFrame(frame);
      running = false;
      resizeObserver.disconnect();
      visibility.disconnect();
      document.removeEventListener("visibilitychange", onVisibilityChange);
      canvas.removeEventListener("webglcontextlost", onContextLost);
      rig.destroy();
      labels.destroy();

      for (const built of resident.values()) built.dispose();
      resident.clear();

      // The shared resources, disposed exactly once and only here.
      columnGeometry.dispose();
      columnMaterial.dispose();
      outline.geometry.dispose();
      outline.material.dispose();
      for (const material of Object.values(lineMaterials)) material.dispose();

      scene.clear();
      renderer.dispose();
      renderer.forceContextLoss();
    },
  };
}
