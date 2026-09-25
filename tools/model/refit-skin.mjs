#!/usr/bin/env node
/**
 * Re-fits the skin inside an already-built body.glb, without the source assets.
 *
 * build-body.mjs is the way to make the file. This is the repair for when the fit
 * inside a shipped file is wrong and the sources are gone: the skin geometry is
 * intact, only its wrapper node's transform is off, so recompute that and nothing
 * else.
 *
 * Run it from the repository root:
 *
 *   node tools/model/refit-skin.mjs [--in web/assets/models/body.glb] [--out <same>]
 *     [--skin-scale 1] [--skin-offset 0,0,0]
 */
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

import { NodeIO } from "@gltf-transform/core";
import { ALL_EXTENSIONS } from "@gltf-transform/extensions";
import { MeshoptDecoder, MeshoptEncoder } from "meshoptimizer";

import { assertFit, skinTransform, unionBounds } from "./fit.mjs";

const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const SKIN_NODE = "skin";

function parseArgs(argv) {
  const args = { in: "web/assets/models/body.glb", out: null, skinScale: 1, skinOffset: [0, 0, 0] };
  for (let i = 0; i < argv.length; i += 2) {
    const [flag, value] = [argv[i], argv[i + 1]];
    if (value === undefined) throw new Error(`${flag} needs a value`);
    if (flag === "--in" || flag === "--out") args[flag.slice(2)] = value;
    else if (flag === "--skin-scale") args.skinScale = Number(value);
    else if (flag === "--skin-offset") args.skinOffset = value.split(",").map(Number);
    else throw new Error(`unknown flag "${flag}"`);
  }
  args.out ??= args.in;
  return args;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  await MeshoptDecoder.ready;
  await MeshoptEncoder.ready;
  const io = new NodeIO()
    .registerExtensions(ALL_EXTENSIONS)
    .registerDependencies({ "meshopt.decoder": MeshoptDecoder, "meshopt.encoder": MeshoptEncoder });

  const doc = await io.read(resolve(REPO_ROOT, args.in));
  const scene = doc.getRoot().getDefaultScene() || doc.getRoot().listScenes()[0];
  const children = scene.listChildren();
  const skin = children.find((node) => node.getName() === SKIN_NODE);
  if (!skin) throw new Error(`no top-level "${SKIN_NODE}" node in ${args.in}`);

  const before = skin.getScale()[0];
  skin.setScale([1, 1, 1]).setTranslation([0, 0, 0]).setRotation([0, 0, 0, 1]);

  const muscleBounds = unionBounds(children.filter((node) => node !== skin));
  const { scale, translation } = skinTransform(unionBounds([skin]), muscleBounds, args);
  skin.setScale([scale, scale, scale]).setTranslation(translation);
  const ratio = assertFit(skin, muscleBounds);

  await io.write(resolve(REPO_ROOT, args.out), doc);
  console.log(`skin scale ${before.toFixed(4)} -> ${scale.toFixed(4)}, height ratio ${ratio.toFixed(3)}`);
  console.log(`wrote ${args.out}`);
}

main().catch((error) => {
  console.error(`refit-skin: ${error.message}`);
  process.exit(1);
});
