/**
 * A week of terrain, as GPU objects.
 *
 * One InstancedMesh per week, seven instances wide. Per-week rather than one
 * mesh for the whole landscape because a week is the unit that loads and the
 * unit that scrolls away: recycling slots inside one enormous buffer would be
 * fewer draw calls and a much harder allocator, and twenty resident weeks is
 * twenty draws, which is nothing.
 *
 * What this module must never do is dispose a shared resource. The box
 * geometry, the column material and the per-family line materials belong to
 * the scene and outlive every week. Disposing them when a week is evicted
 * would leave every week built afterwards rendering black — which is exactly
 * what the old viewer's single flat `disposables` array would have done the
 * moment anything was removed.
 */
import * as THREE from "/assets/js/vendor/three.module.min.js";

import { loadToHeight, clipped, COLUMN_SIZE, DAY_PITCH, PLATE_HEIGHT, WEEK_PITCH } from "./geo.js";
import { colorFor } from "./palette.js";
import * as routes from "./routes.js";

// The grid itself lives in geo.js, with the rest of the arithmetic that has no
// business importing a renderer. Re-exported here because this is where anyone
// building geometry looks for it.
export { COLUMN_SIZE, DAY_PITCH, WEEK_PITCH } from "./geo.js";

const DAYS = 7;

// A mixed day is drawn in its dominant colour, desaturated, so it reads as
// "mostly this" rather than "this". The stacked column is the only fully
// honest answer; this is the second best, and the tooltip carries the rest.
const MIXED_DESATURATION = 0.45;

/**
 * Builds one week. `offset` is how many weeks back from the newest it sits,
 * which is what places it along the travel axis.
 */
export function buildWeek(week, offset, { palette, geometry, material, lineMaterials, loadScale }) {
  const group = new THREE.Group();
  group.position.z = -offset * WEEK_PITCH;

  const columns = new THREE.InstancedMesh(geometry, material, DAYS);
  columns.castShadow = true;
  columns.receiveShadow = true;
  // The instance colours change only when a week is built, never per frame.
  columns.instanceMatrix.setUsage(THREE.StaticDrawUsage);

  const matrix = new THREE.Matrix4();
  const color = new THREE.Color();
  const days = [];

  week.days.forEach((day, index) => {
    const height = loadToHeight(day.load_met_min, loadScale);
    const x = (index - (DAYS - 1) / 2) * DAY_PITCH;

    // Scaled from the origin then lifted by half, so the column grows upward
    // from the ground rather than through it.
    matrix.makeScale(COLUMN_SIZE, height, COLUMN_SIZE);
    matrix.setPosition(x, height / 2, 0);
    columns.setMatrixAt(index, matrix);

    if (day.sessions === 0) {
      color.copy(palette.rest);
      // A day that has not happened is drawn fainter still. It is ground the
      // week needs in order to be a week, not a choice anybody made.
      if (day.future) color.lerp(palette.background, 0.55);
    } else {
      color.copy(colorFor(palette, day.dominant));
      if (day.mixed) {
        // Toward the page colour rather than toward grey, so a mixed day
        // still reads as its family in both themes.
        color.lerp(palette.background, MIXED_DESATURATION);
      }
    }
    columns.setColorAt(index, color);

    days.push({
      day,
      index,
      x,
      height,
      clipped: clipped(day.load_met_min, loadScale),
      worldZ: group.position.z,
    });
  });

  columns.instanceMatrix.needsUpdate = true;
  if (columns.instanceColor) columns.instanceColor.needsUpdate = true;
  group.add(columns);

  // Routes sit on the cap of the day they happened on.
  const lines = [];
  week.days.forEach((day, index) => {
    if (!Array.isArray(day.routes)) return;
    for (const route of day.routes) {
      const line = routes.build(route, { palette, materials: lineMaterials, size: COLUMN_SIZE });
      if (!line) continue;
      line.position.set(days[index].x, days[index].height + PLATE_HEIGHT, 0);
      group.add(line);
      lines.push(line);
    }
  });

  return {
    start: week.start,
    offset,
    group,
    columns,
    days,
    lines,

    // Drops only what this week owns. The geometry and the materials are the
    // scene's, and are still in use by every other week.
    dispose() {
      columns.dispose();
      for (const line of lines) line.userData.geometry.dispose();
      group.clear();
    },
  };
}
