/**
 * The camera rig: an offset from a target that travels along the week axis,
 * with a clamped yaw.
 *
 * Deliberately not a free orbit. The old scene let you spin the field on Y
 * without limit, which was the right control for an arbitrary grid where the
 * ground axes meant nothing. Here they are weekday and week, and a landscape
 * viewed edge-on or from behind stops being a calendar — so yaw is clamped and
 * pitch is fixed.
 *
 * The offset is computed from the content and the canvas, not written down.
 * It used to be two constants — DISTANCE 9.5 and HEIGHT 6.2 — chosen against a
 * canvas that filled the viewport, and they were wrong even there: the frame's
 * top edge sat about 17 degrees below the horizon while a full-height column
 * reached 8, so every hard day was cut off above the shoulder. A constant
 * cannot be right for both a 68vh hero and the banner this is now.
 *
 * geo.frame() solves for the distance that keeps the content inside the
 * frustum at whatever shape the canvas happens to be. It lives there, with the
 * rest of the arithmetic that needs no renderer, so the fit can be tested
 * without a GPU — which is what nobody could do to the two constants.
 *
 * The wheel is not taken. A canvas that calls preventDefault on wheel events
 * would trap the page: the list below it could never be scrolled to. Travel is
 * by drag.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

import { AIM, FOV, PITCH, WEEK_PITCH, frame } from "./geo.js";

const MAX_YAW = 0.61; // ~35°, enough to look along the terrain, not past it

// A drag of this many pixels is a drag; anything less was a click. Carried
// over from the old viewer, where it was the one interaction detail worth
// keeping.
const CLICK_SLOP = 6;

const DRAG_TO_YAW = 0.006;
const DRAG_TO_TRAVEL = 0.018;

export function createCameraRig(canvas, { reduced, onTravel, onClick, onHoverMove }) {
  const camera = new THREE.PerspectiveCamera(FOV, 1, 0.1, 400);

  let yaw = 0;
  let targetYaw = 0;
  let week = 0; // how many weeks back the camera is looking
  let targetWeek = 0;
  let maxWeek = 0;
  let distance = frame(1);

  let dragging = false;
  let last = { x: 0, y: 0 };
  let down = { x: 0, y: 0 };
  let moved = 0;

  const onPointerDown = (e) => {
    dragging = true;
    last = { x: e.clientX, y: e.clientY };
    down = { x: e.clientX, y: e.clientY };
    moved = 0;
    canvas.setPointerCapture(e.pointerId);
  };

  const onPointerMove = (e) => {
    if (!dragging) {
      onHoverMove(e);
      return;
    }

    const dx = e.clientX - last.x;
    const dy = e.clientY - last.y;
    last = { x: e.clientX, y: e.clientY };
    moved = Math.hypot(e.clientX - down.x, e.clientY - down.y);

    targetYaw = clamp(targetYaw + dx * DRAG_TO_YAW, -MAX_YAW, MAX_YAW);

    // Dragging down pulls the landscape toward you, which is travelling
    // forward in time; dragging up pushes into the past.
    targetWeek = clamp(targetWeek - dy * DRAG_TO_TRAVEL, 0, maxWeek);
    onTravel(targetWeek);
  };

  const onPointerUp = (e) => {
    dragging = false;
    if (e.pointerId !== undefined && canvas.hasPointerCapture(e.pointerId)) {
      canvas.releasePointerCapture(e.pointerId);
    }
    if (moved <= CLICK_SLOP) onClick(e);
  };

  canvas.addEventListener("pointerdown", onPointerDown);
  canvas.addEventListener("pointermove", onPointerMove);
  canvas.addEventListener("pointerup", onPointerUp);
  canvas.addEventListener("pointercancel", onPointerUp);

  function clamp(v, lo, hi) {
    return Math.min(Math.max(v, lo), hi);
  }

  function place() {
    const target = new THREE.Vector3(0, AIM, -week * WEEK_PITCH);
    const ground = distance * Math.cos(PITCH);

    camera.position.set(
      target.x + Math.sin(yaw) * ground,
      target.y + distance * Math.sin(PITCH),
      target.z + Math.cos(yaw) * ground,
    );
    camera.lookAt(target);
  }

  return {
    camera,

    // setRange says how far back there is ground to travel over. Travel is
    // clamped to it, so reaching the end of the record stops the camera
    // rather than flying off into empty space.
    setRange(weeks) {
      maxWeek = Math.max(0, weeks - 1);
      targetWeek = clamp(targetWeek, 0, maxWeek);
    },

    travelTo(weekOffset) {
      targetWeek = clamp(weekOffset, 0, maxWeek);
      if (reduced) week = targetWeek; // snap: no glide for reduced motion
      onTravel(targetWeek);
    },

    at() {
      return week;
    },

    // distanceToTarget is what the fog is hung on: haze has to start beyond
    // the ground anybody is looking at, and that ground moves when the canvas
    // changes shape.
    distanceToTarget() {
      return distance;
    },

    // update eases toward the target and returns whether anything moved, so
    // the scene can stop rendering when the landscape is still.
    update() {
      const before = { yaw, week };

      if (reduced) {
        yaw = targetYaw;
        week = targetWeek;
      } else {
        yaw += (targetYaw - yaw) * 0.12;
        week += (targetWeek - week) * 0.12;
      }

      place();

      return Math.abs(before.yaw - yaw) > 1e-4 || Math.abs(before.week - week) > 1e-4;
    },

    resize(width, height) {
      camera.aspect = width / height;
      distance = frame(camera.aspect);
      camera.updateProjectionMatrix();
      place();
    },

    destroy() {
      canvas.removeEventListener("pointerdown", onPointerDown);
      canvas.removeEventListener("pointermove", onPointerMove);
      canvas.removeEventListener("pointerup", onPointerUp);
      canvas.removeEventListener("pointercancel", onPointerUp);
    },
  };
}
