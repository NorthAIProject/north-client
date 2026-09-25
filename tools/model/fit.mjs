/**
 * The skin-to-muscle fit, shared by build-body.mjs and refit-skin.mjs.
 *
 * Bounds here are always world-space (getBounds walks node transforms). Raw
 * POSITION min/max is not good enough: the Sketchfab skin nests its mesh under
 * rotated and scaled nodes, and fitting against the raw accessor once shipped a
 * skin ~170x too small — invisible, and with it every muscle, since the glow only
 * draws behind skin depth.
 */
import { getBounds } from "@gltf-transform/core";

/** World-space bounds of several nodes (or scenes) together. */
export function unionBounds(targets) {
  const min = [Infinity, Infinity, Infinity];
  const max = [-Infinity, -Infinity, -Infinity];
  for (const target of targets) {
    const b = getBounds(target);
    for (let axis = 0; axis < 3; axis += 1) {
      min[axis] = Math.min(min[axis], b.min[axis]);
      max[axis] = Math.max(max[axis], b.max[axis]);
    }
  }
  return { min, max };
}

/**
 * Scale and translation for the skin wrapper: match overall height, centre the
 * footprint, and stand the feet on the muscles' floor rather than aligning centres —
 * a difference in leg length should show up at the head, not push the model through
 * the contact shadow. skinBounds must be measured with the wrapper at identity.
 */
export function skinTransform(skinBounds, muscleBounds, { skinScale = 1, skinOffset = [0, 0, 0] } = {}) {
  const height = (b) => b.max[1] - b.min[1];
  const scale = (height(muscleBounds) / height(skinBounds)) * skinScale;
  const centre = (b, axis) => (b.min[axis] + b.max[axis]) / 2;

  return {
    scale,
    translation: [
      centre(muscleBounds, 0) - centre(skinBounds, 0) * scale + skinOffset[0],
      muscleBounds.min[1] - skinBounds.min[1] * scale + skinOffset[1],
      centre(muscleBounds, 2) - centre(skinBounds, 2) * scale + skinOffset[2],
    ],
  };
}

/**
 * Throws unless the fitted skin is about as tall as the muscles. --skin-scale
 * legitimately nudges it a few percent; anything past 10% is a broken fit.
 */
export function assertFit(skinWrapper, muscleBounds) {
  const skin = getBounds(skinWrapper);
  const ratio = (skin.max[1] - skin.min[1]) / (muscleBounds.max[1] - muscleBounds.min[1]);
  if (!(ratio > 0.9 && ratio < 1.1)) {
    throw new Error(
      `fitted skin is ${ratio.toFixed(3)}x the muscle height (want ~1.0) — ` +
        "the fit is wrong; check the skin source's node transforms",
    );
  }
  return ratio;
}
