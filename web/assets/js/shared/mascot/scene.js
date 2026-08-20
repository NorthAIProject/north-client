/**
 * The mascot's scene and its states (NOR-51).
 *
 * States are pose targets, not animation clips. Each of idle/thinking/
 * listening/celebrate/nod is a table of scalars — head tilt, eyelid, arm
 * raise, glow — and a critically-damped spring drives the current value
 * toward the target every frame. That is why states can interrupt each other
 * mid-motion without popping, and why there is no AnimationMixer here: a
 * mixer would need clips, clips would need a rig, and a rig would need the GLB
 * this component deliberately does not have.
 *
 * On top of the pose sits a per-state function of time. The pose says where
 * the mascot is; the time function is what makes it look alive.
 *
 * Performance contract (see README.md in this directory): DPR capped at 1.5,
 * no loop under prefers-reduced-motion, and nothing renders while the tab is
 * hidden. Mount/unmount is the caller's job — alpine.js drives it from an
 * IntersectionObserver.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

import { buildMascot, readMascotPalette } from "./model.js";

export const STATES = ["idle", "thinking", "listening", "celebrate", "nod"];

// celebrate and nod are gestures: they play, then hand control back to
// whatever sustained state was showing underneath.
const ONE_SHOT = { celebrate: 1.4, nod: 0.9 };

export function isOneShot(state) {
  return Object.prototype.hasOwnProperty.call(ONE_SHOT, state);
}

// Rest values. Anything a state does not mention returns here, which keeps the
// tables below short enough to read as intent rather than as configuration.
const REST = {
  headPitch: 0,
  headRoll: 0,
  headForward: 0,
  bodyY: 0,
  eyeLid: 1,
  armRaise: 0,
  glow: 1,
};

const POSES = {
  idle: {},
  thinking: { headRoll: 0.22, headPitch: 0.1, eyeLid: 0.55, glow: 1.5 },
  listening: { headPitch: -0.12, headForward: 0.14, bodyY: 0.05, glow: 1.35 },
  celebrate: { armRaise: 1, headPitch: -0.18, glow: 2.1 },
  nod: { glow: 1.15 },
};

/**
 * Critically damped spring: no overshoot, no oscillation, and it converges
 * from any starting velocity — which is what makes an interrupted state look
 * deliberate instead of snapped.
 */
function spring(value, velocity, target, stiffness, dt) {
  const accel = -2 * stiffness * velocity - stiffness * stiffness * (value - target);
  const nextVelocity = velocity + accel * dt;
  return [value + nextVelocity * dt, nextVelocity];
}

/**
 * @param {HTMLCanvasElement} canvas
 * @param {{reduced?: boolean, state?: string}} options
 */
export function createMascot(canvas, options = {}) {
  const reduced = Boolean(options.reduced);

  const renderer = new THREE.WebGLRenderer({
    canvas,
    alpha: true, // the mascot sits on the page colour, not on its own backdrop
    antialias: true,
    powerPreference: "low-power",
  });
  renderer.setClearAlpha(0);

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(32, 1, 0.1, 50);
  camera.position.set(0, 1.0, 6.4);
  camera.lookAt(0, 0.85, 0);

  // Three lights, no environment map. RoomEnvironment is vendored, but a PMREM
  // render per mount is disproportionate for a two-thousand-triangle toy that
  // may appear four times on one page.
  const palette = readMascotPalette();
  const key = new THREE.DirectionalLight(0xffffff, 2.1);
  key.position.set(2.4, 3.2, 3.0);
  const fill = new THREE.AmbientLight(0xffffff, 0.55);
  const rim = new THREE.DirectionalLight(palette.signal, 1.5);
  rim.position.set(-2.6, 1.2, -2.2);
  scene.add(key, fill, rim);

  const mascot = buildMascot(palette);
  scene.add(mascot.root);

  // Current, velocity, and target for every scalar in REST.
  const current = { ...REST };
  const velocity = {};
  const target = { ...REST };
  for (const name of Object.keys(REST)) velocity[name] = 0;

  let sustained = STATES.includes(options.state) && !isOneShot(options.state)
    ? options.state
    : "idle";
  let gesture = null;
  let gestureLeft = 0;
  let clock = 0;
  let frame = null;
  let last = 0;
  let disposed = false;

  applyTargets(gesture || sustained);

  function applyTargets(state) {
    const pose = POSES[state] || {};
    for (const name of Object.keys(REST)) {
      target[name] = Object.prototype.hasOwnProperty.call(pose, name) ? pose[name] : REST[name];
    }
  }

  function resize() {
    const width = canvas.clientWidth || 1;
    const height = canvas.clientHeight || 1;
    const dpr = Math.min(window.devicePixelRatio || 1, 1.5);

    renderer.setPixelRatio(dpr);
    renderer.setSize(width, height, false);
    camera.aspect = width / height;
    camera.updateProjectionMatrix();
  }

  /**
   * The part of the motion that is a function of time rather than of pose:
   * breathing, float, the hop, the nod itself.
   */
  function animate(state, elapsed, dt) {
    const { head, torso, armL, armR, glyph, speckles } = mascot.parts;

    // Idle float and breathe, always present so the mascot never looks frozen
    // while it is showing a sustained state.
    const breathe = Math.sin(elapsed * 1.6) * 0.02;
    const float = Math.sin(elapsed * 1.55) * 0.035;

    let hop = 0;
    if (state === "celebrate") {
      // Two bounces over the gesture, decaying — a small hop, not a jump.
      const progress = 1 - gestureLeft / ONE_SHOT.celebrate;
      hop = Math.abs(Math.sin(progress * Math.PI * 2)) * 0.34 * (1 - progress * 0.45);
    }

    let nod = 0;
    if (state === "nod") {
      const progress = 1 - gestureLeft / ONE_SHOT.nod;
      nod = Math.sin(progress * Math.PI * 2) * 0.34 * (1 - progress);
    }

    mascot.root.position.y = current.bodyY + float + hop;

    head.rotation.x = current.headPitch + nod;
    head.rotation.z = current.headRoll;
    head.position.z = current.headForward;
    head.position.y = 1.2 + breathe;

    torso.scale.set(1 + breathe * 0.6, 1 - breathe * 0.4, 1 + breathe * 0.6);

    armL.rotation.z = 0.55 + current.armRaise * 1.5;
    armR.rotation.z = -0.55 - current.armRaise * 1.5;

    mascot.parts.eyeL.scale.y = current.eyeLid;
    mascot.parts.eyeR.scale.y = current.eyeLid;

    // The tell. Thinking pulses; every other state holds steady at its level.
    const pulse = state === "thinking" ? 1 + Math.sin(elapsed * 4.2) * 0.28 : 1;
    const intensity = current.glow * pulse;
    mascot.materials.glow.opacity = Math.min(1, 0.55 * intensity);
    mascot.materials.speckleMaterial.opacity = Math.min(1, 0.5 * intensity);
    mascot.materials.body.emissiveIntensity = 0.12 * intensity;

    glyph.rotation.z = elapsed * 0.35;
    speckles.rotation.y = elapsed * 0.12;
  }

  function step(now) {
    if (disposed) return;

    const dt = Math.min((now - last) / 1000 || 0, 0.05);
    last = now;
    clock += dt;

    if (gesture) {
      gestureLeft -= dt;
      if (gestureLeft <= 0) {
        gesture = null;
        applyTargets(sustained);
      }
    }

    const state = gesture || sustained;
    for (const name of Object.keys(REST)) {
      const [value, v] = spring(current[name], velocity[name], target[name], 9, dt);
      current[name] = value;
      velocity[name] = v;
    }

    animate(state, clock, dt);
    render();

    frame = requestAnimationFrame(step);
  }

  function render() {
    resize();
    renderer.render(scene, camera);
  }

  function start() {
    if (reduced || frame !== null || disposed) return;
    last = performance.now();
    frame = requestAnimationFrame(step);
  }

  function stop() {
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
  }

  // Under reduced motion the mascot is a still image that changes pose when
  // asked: settle the springs instantly and draw exactly one frame. Same
  // render-on-demand choice scroll-world/world.js makes.
  function settleAndDraw() {
    for (const name of Object.keys(REST)) {
      current[name] = target[name];
      velocity[name] = 0;
    }
    animate(gesture || sustained, 0, 0);
    render();
  }

  function onVisibility() {
    if (document.hidden) stop();
    else start();
  }
  document.addEventListener("visibilitychange", onVisibility);

  if (reduced) settleAndDraw();
  else start();

  return {
    setState(name) {
      if (disposed || !STATES.includes(name)) return;

      if (isOneShot(name)) {
        // Gestures are motion and nothing else, so under reduced motion they
        // are simply not played. Nothing ticks the clock down in that mode,
        // so starting one would latch the pose permanently.
        if (reduced) return;
        gesture = name;
        gestureLeft = ONE_SHOT[name];
      } else {
        sustained = name;
        // A sustained change during a gesture updates what the gesture will
        // fall back to, without cutting the gesture short.
        if (gesture) return;
      }

      applyTargets(gesture || sustained);
      if (reduced) settleAndDraw();
    },

    destroy() {
      disposed = true;
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
      mascot.dispose();
      renderer.dispose();
    },
  };
}
