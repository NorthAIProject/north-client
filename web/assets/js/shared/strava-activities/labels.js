/**
 * Axis labels, as DOM rather than as textures.
 *
 * A CanvasTexture per label would mean an upload per week and blurry type at
 * every distance. These are ordinary elements positioned from projected world
 * coordinates, so they get real font rendering and the product's own mono
 * treatment — the same one every other instrument label uses.
 *
 * Because they are DOM, they have no depth test: a label projected onto a spot
 * a column happens to occupy draws on top of it, which is how the week dates
 * ended up floating in the middle of the landscape. Two rules keep them out of
 * it — the week dates are pinned to a gutter down the left-hand edge, and the
 * weekday row sits in front of the nearest terrain rather than on top of it.
 *
 * The whole overlay is aria-hidden. The week list below the scene is the
 * accessible equivalent, and a screen reader reading "M T W T F S S" on a
 * loop would be noise, not navigation.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

import { DAY_PITCH, WEEK_PITCH } from "./terrain.js";

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

// How many weeks get a date. The rig frames about thirteen; labelling four of
// them, as this did, left most of the landscape undated.
const LABELLED_WEEKS = 12;

// Two labels closer together than this are one illegible smudge, so the
// further of the pair is dropped. Rows compress toward the horizon, which is
// what makes a fixed count of labels wrong on its own.
const MIN_LABEL_GAP = 14;

// The gutter the week dates live in: a fixed inset from the left edge, capped
// as a fraction of width so a narrow canvas cannot push them over the terrain.
const GUTTER = 10;
const GUTTER_MAX_FRACTION = 0.22;

// How far in front of the camera's target the weekday row sits. It has to
// clear the nearest resident week, or it draws over the columns it labels.
const WEEKDAY_LEAD = 2.9;

// The world x the week dates are projected from: just outside Monday.
const ROW_EDGE = -4.4 * DAY_PITCH;

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

      // Weekday labels ride along the near edge of the landscape, so they stay
      // readable as it travels.
      const nearZ = (-cameraWeek + WEEKDAY_LEAD) * WEEK_PITCH;
      weekdayNodes.forEach((el, i) => {
        const x = (i - 3) * DAY_PITCH;
        const at = project(camera, x, 0, nearZ, width, height);
        el.style.transform = `translate(${at.x}px, ${at.y}px)`;
        el.hidden = !at.visible || at.y < 0 || at.y > height;
      });

      while (weekNodes.length < LABELLED_WEEKS) {
        const el = document.createElement("span");
        el.className = `absolute -translate-y-1/2 ${MONO}`;
        host.appendChild(el);
        weekNodes.push(el);
      }

      const gutter = Math.min(GUTTER, width * GUTTER_MAX_FRACTION);
      const nearest = Math.round(cameraWeek);
      let lastY = -Infinity;

      weekNodes.forEach((el, i) => {
        const index = weeks.length - 1 - (nearest + i);
        const week = weeks[index];
        if (!week) {
          el.hidden = true;
          return;
        }

        const at = project(camera, ROW_EDGE, 0, -(nearest + i) * WEEK_PITCH, width, height);
        if (!at.visible || at.y < 0 || at.y > height || Math.abs(at.y - lastY) < MIN_LABEL_GAP) {
          el.hidden = true;
          return;
        }
        lastY = at.y;

        // x is the gutter, not where the row's edge projected. The edge
        // slides under the terrain as the camera yaws, and a label with no
        // depth test follows it straight over the columns — which is how these
        // dates ended up floating in the middle of the landscape. Only the
        // height is taken from the projection, so the dates still line up with
        // the rows they name.
        el.textContent = week.label;
        el.style.transform = `translate(${gutter}px, ${at.y}px)`;
        el.hidden = false;
      });
    },

    destroy() {
      host.innerHTML = "";
    },
  };
}
