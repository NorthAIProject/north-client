/**
 * The scroll world: one WebGL canvas behind the marketing page.
 *
 * See ./README.md for what it is trying to say and which shipped assets it uses.
 * This file owns the renderer, the camera's trip along the bearing line, and the
 * rules about when it is allowed to spend a frame.
 *
 * It is decoration in the strict sense — the page has to be complete without it.
 * Nothing here is in the tab order, nothing here carries information that is not
 * also in the copy, and every failure path ends with an empty div.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";
import { readPalette } from "./palette.js";
import {
  createBearingPath,
  createLine,
  createMotes,
  createBrackets,
  createRails,
  createWaypoints,
} from "./geometry.js";
import { createFilmPanel, createMascot } from "./panel.js";

// Below this the world is not built at all. A phone renders the same number of
// pixels as a laptop on a quarter of the power budget, and the page's argument
// is carried by the sections, not by what is behind them.
const MIN_WIDTH = 768;

// Where along the path each set-piece stands.
const FILM_AT = 0.07;
const MASCOT_AT = 0.52;

/**
 * @param {HTMLElement} host   empty div, already positioned fixed behind the page
 * @param {object} options
 * @param {boolean} options.reduced
 * @returns {{update: (progress: number, muscleActive: boolean) => void, destroy: () => void} | null}
 */
export function createWorld(host, { reduced = false } = {}) {
  if (window.innerWidth < MIN_WIDTH) return null;

  const renderer = new THREE.WebGLRenderer({
    antialias: true,
    alpha: true,
    powerPreference: "high-performance",
  });
  // 1.5 rather than the device's own ratio: past that, nothing in this scene is
  // fine enough to resolve, and a 3x phone-class display would be shading nine
  // times the fragments for a hairline that is one pixel wide either way.
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 1.5));
  renderer.setSize(host.clientWidth, host.clientHeight);
  renderer.setClearAlpha(0);
  host.appendChild(renderer.domElement);

  const palette = readPalette();

  const scene = new THREE.Scene();
  // The fog colour is the page background, so the line does not fade out into a
  // grey that no other part of the page contains — it dissolves into the section
  // it is sitting behind. The density is loose enough to keep three or four
  // waypoints in frame at once: denser fog makes a prettier single shot and
  // destroys the one thing the composition is for, which is seeing how much line
  // is already behind you.
  scene.fog = new THREE.FogExp2(palette.background, 0.015);

  const camera = new THREE.PerspectiveCamera(52, aspect(), 0.1, 400);

  const curve = createBearingPath();

  // Waypoints come from the page's own structure. `sections` is supplied by
  // scroll.js and is whatever `main section` matched — see geometry.js.
  let fractions = [];

  const line = createLine(curve, palette);
  const motes = createMotes(curve, palette);
  const rails = createRails(curve, palette);
  scene.add(line.line, rails, motes);

  let waypoints = null;
  let brackets = null;

  // Under reduced motion there is no loop, so anything that finishes loading
  // after the single frame has to ask for another one or it is never seen.
  const film = createFilmPanel(filmPosition(), palette, reduced, () => {
    if (reduced && running) renderer.render(scene, camera);
  });
  scene.add(film.group);

  let mascot = null;
  let mascotRequested = false;

  // ---------------------------------------------------------------------------
  // Camera
  // ---------------------------------------------------------------------------

  const at = new THREE.Vector3();
  const ahead = new THREE.Vector3();
  const tangent = new THREE.Vector3();
  const side = new THREE.Vector3();
  const WORLD_UP = new THREE.Vector3(0, 1, 0);

  /**
   * A chase camera, offset from the line rather than sitting on it — you cannot
   * see a path you are standing in the middle of.
   *
   * The offset is built in the path's own frame (behind along the tangent, out
   * along its side) rather than along the world axes. With a fixed world offset
   * the line drifts in and out of shot every time the path sways, which reads as
   * the camera wandering rather than as the line curving.
   *
   * The lateral distance widens as the journey goes on. That is the "month eight"
   * shot: rather than spinning the camera round to look back, which is
   * disorienting mid-scroll, it swings further out so more of the line already
   * travelled stays in frame. The accumulation is the argument, so it has to be
   * visible without the visitor having to do anything.
   */
  function placeCamera(p) {
    const t = clamp01(p);
    curve.getPointAt(t, at);
    // Far enough ahead that the view runs down the corridor. Aiming just in
    // front of the camera instead points it across the path, and the line
    // leaves frame at the first bend.
    curve.getPointAt(clamp01(t + 0.09), ahead);
    curve.getTangentAt(t, tangent);
    side.crossVectors(tangent, WORLD_UP).normalize();

    const out = 1.8 + t * 4.2;
    const up = 1.0 + t * 1.1;
    const back = 5.0;

    camera.position
      .copy(at)
      .addScaledVector(side, out)
      .addScaledVector(WORLD_UP, up)
      .addScaledVector(tangent, -back);
    camera.lookAt(ahead);
  }

  function filmPosition() {
    const point = curve.getPointAt(FILM_AT, new THREE.Vector3());
    return point.add(new THREE.Vector3(-6.5, 2.2, 0));
  }

  // ---------------------------------------------------------------------------
  // Frame loop
  // ---------------------------------------------------------------------------

  let target = 0; // where the page says we are
  let current = 0; // where the camera has caught up to
  let muscleActive = false;
  let frame = null;
  let lastDraw = 0;
  let running = false;
  let lost = false;

  function draw(time) {
    frame = requestAnimationFrame(draw);
    if (document.hidden || lost) return;

    // While the muscle demo is on screen it owns a second WebGL context and the
    // visitor's attention. Two full-rate render loops on one page is the most
    // likely source of jank here, so this one steps back to roughly 20fps and
    // fades down. It is not paused outright: a world that freezes and then jumps
    // when you scroll past reads worse than one that is quietly slower.
    if (muscleActive && time - lastDraw < 50) return;
    lastDraw = time;

    // Damped follow. Lenis already smooths the scroll position; this smooths the
    // camera's reaction to it, which is what keeps a fast flick from snapping.
    current += (target - current) * 0.09;
    if (Math.abs(target - current) < 0.00005) current = target;

    placeCamera(current);
    line.update(current);
    if (waypoints) waypoints.update(current);
    film.update();

    if (mascot) {
      // A slow bob, so the companion reads as present rather than pinned to a
      // coordinate. Skipped entirely under reduced motion — see start().
      mascot.sprite.position.y = mascotBaseY + Math.sin(time * 0.0009) * 0.35;
    }

    // The motes turn about the path rather than translating, which reads as
    // drift without anything ever leaving the volume they were seeded in.
    motes.rotation.z = time * 0.000012;

    renderer.render(scene, camera);
  }

  let mascotBaseY = 0;

  function start() {
    if (running) return;
    running = true;
    if (reduced) {
      // One frame, then nothing. The composition is kept; the motion is not.
      current = target;
      placeCamera(current);
      line.update(current);
      if (waypoints) waypoints.update(current);
      renderer.render(scene, camera);
      return;
    }
    frame = requestAnimationFrame(draw);
  }

  function stop() {
    running = false;
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
  }

  // ---------------------------------------------------------------------------
  // Host events
  // ---------------------------------------------------------------------------

  function aspect() {
    return host.clientWidth / Math.max(1, host.clientHeight);
  }

  function onResize() {
    // Not destroyed below MIN_WIDTH on the way down: tearing down and rebuilding
    // a scene on a window drag is worse than letting an already-built one run.
    renderer.setSize(host.clientWidth, host.clientHeight);
    camera.aspect = aspect();
    camera.updateProjectionMatrix();
    if (reduced) renderer.render(scene, camera);
  }
  window.addEventListener("resize", onResize, { passive: true });

  // A lost context leaves the page exactly as it is without the world — the host
  // is an empty div and no layout depends on it. Restoring would mean rebuilding
  // every buffer for something the page does not need, so it is not attempted.
  function onContextLost(event) {
    event.preventDefault();
    lost = true;
    stop();
    host.style.opacity = "0";
  }
  renderer.domElement.addEventListener("webglcontextlost", onContextLost);

  function onThemeChange() {
    // Tokens are read live, so a theme switch means new colours everywhere.
    const next = readPalette();
    Object.assign(palette, next);
    scene.fog.color.copy(next.background);
    rails.material.color.copy(next.hairline);
    if (brackets) brackets.material.color.copy(next.hairline);
    // Both of these cache what they last drew, so they are forced to treat the
    // next update as a change rather than as a repeat of the same picture.
    line.update(-1);
    if (waypoints) waypoints.update(-1);
  }
  document.addEventListener("theme-changed", onThemeChange);

  return {
    /**
     * Hands the world the page's own structure. Called once, after scroll.js has
     * measured the sections.
     * @param {number[]} sectionFractions midpoint of each section, 0..1
     */
    setSections(sectionFractions) {
      fractions = sectionFractions;
      if (waypoints) {
        scene.remove(waypoints.mesh);
        waypoints.dispose();
      }
      if (brackets) {
        scene.remove(brackets);
        brackets.geometry.dispose();
        brackets.material.dispose();
      }
      waypoints = createWaypoints(curve, fractions, palette);
      brackets = createBrackets(curve, fractions, palette);
      scene.add(waypoints.mesh, brackets);
      start();
    },

    update(progress, isMuscleActive) {
      target = clamp01(progress);
      muscleActive = Boolean(isMuscleActive);
      host.style.opacity = muscleActive ? "0.15" : "1";

      if (target > FILM_AT - 0.05 && target < FILM_AT + 0.25) film.activate();

      // The mascot is 416KB. It is fetched when the camera is close enough that
      // it will actually be seen, and never on the initial page load.
      if (!mascotRequested && target > MASCOT_AT - 0.2) {
        mascotRequested = true;
        const position = curve.getPointAt(MASCOT_AT, new THREE.Vector3());
        position.add(new THREE.Vector3(5.2, 1.0, 0));
        mascotBaseY = position.y;
        createMascot(position).then((loaded) => {
          if (!loaded) return;
          mascot = loaded;
          scene.add(loaded.sprite);
          if (reduced) renderer.render(scene, camera);
        });
      }

      if (reduced && running) {
        // No loop under reduced motion, so a scroll has to ask for its own frame.
        current = target;
        placeCamera(current);
        line.update(current);
        if (waypoints) waypoints.update(current);
        renderer.render(scene, camera);
      }
    },

    destroy() {
      stop();
      window.removeEventListener("resize", onResize);
      document.removeEventListener("theme-changed", onThemeChange);
      renderer.domElement.removeEventListener("webglcontextlost", onContextLost);
      line.dispose();
      rails.geometry.dispose();
      rails.material.dispose();
      motes.geometry.dispose();
      motes.material.map.dispose();
      motes.material.dispose();
      if (waypoints) waypoints.dispose();
      if (brackets) {
        brackets.geometry.dispose();
        brackets.material.dispose();
      }
      film.dispose();
      if (mascot) mascot.dispose();
      renderer.dispose();
      if (renderer.domElement.parentNode) {
        renderer.domElement.parentNode.removeChild(renderer.domElement);
      }
    },
  };
}

function clamp01(v) {
  return Math.min(1, Math.max(0, v));
}
