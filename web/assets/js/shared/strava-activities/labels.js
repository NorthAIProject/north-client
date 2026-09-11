/**
 * Axis labels, as DOM rather than as textures.
 *
 * A CanvasTexture per label would mean an upload per week and blurry type at
 * every distance. These are ordinary elements positioned from projected world
 * coordinates, so they get real font rendering and the product's own mono
 * treatment — the same one every other instrument label uses.
 *
 * The whole overlay is aria-hidden. The week list below the scene is the
 * accessible equivalent, and a screen reader reading "M T W T F S S" on a
 * loop would be noise, not navigation.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

import { DAY_PITCH } from "./terrain.js";

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

// Only weeks this close to the camera get a date label. Further back they
// would overlap into an unreadable stack.
const LABELLED_WEEKS = 4;

const MONO = "font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground";

export function createLabels(host) {
  if (!host) return { update() {}, destroy() {} };

  host.innerHTML = "";

  const weekdayNodes = WEEKDAYS.map((name) => {
    const el = document.createElement("span");
    el.className = `absolute -translate-x-1/2 ${MONO}`;
    el.textContent = name;
    host.appendChild(el);
    return el;
  });

  const weekNodes = [];
  const vector = new THREE.Vector3();

  function project(camera, x, y, z, width, height) {
    vector.set(x, y, z).project(camera);
    return {
      x: ((vector.x + 1) / 2) * width,
      y: ((1 - vector.y) / 2) * height,
      visible: vector.z < 1,
    };
  }

  return {
    update(camera, weeks, cameraWeek, width, height) {
      if (!width || !height) return;

      // Weekday labels ride along the near edge of whatever week the camera
      // is looking at, so they stay readable as the landscape travels.
      const nearZ = -cameraWeek + 0.9;
      weekdayNodes.forEach((el, i) => {
        const x = (i - 3) * DAY_PITCH;
        const at = project(camera, x, 0, nearZ, width, height);
        el.style.transform = `translate(${at.x}px, ${at.y}px)`;
        el.hidden = !at.visible;
      });

      // One date label per nearby week, on the left-hand end of its row.
      while (weekNodes.length < LABELLED_WEEKS) {
        const el = document.createElement("span");
        el.className = `absolute -translate-y-1/2 ${MONO}`;
        host.appendChild(el);
        weekNodes.push(el);
      }

      const nearest = Math.round(cameraWeek);
      weekNodes.forEach((el, i) => {
        const index = weeks.length - 1 - (nearest + i);
        const week = weeks[index];
        if (!week) {
          el.hidden = true;
          return;
        }

        const at = project(camera, -4.4 * DAY_PITCH, 0, -(nearest + i), width, height);
        el.textContent = week.label;
        el.style.transform = `translate(${at.x}px, ${at.y}px)`;
        el.hidden = !at.visible;
      });
    },

    destroy() {
      host.innerHTML = "";
    },
  };
}
