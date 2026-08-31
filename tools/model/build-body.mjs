#!/usr/bin/env node
/**
 * Builds web/assets/models/body.glb from its two source assets.
 *
 * The viewer needs one file containing two things that ship separately: an
 * anatomical muscle set whose meshes are individually named (so a workout can
 * light up "quads"), and an outer body whose only job is to look like a person.
 * Neither source is usable alone — this script is how they become one asset.
 *
 * Run it from the repository root:
 *
 *   node tools/model/build-body.mjs \
 *     --muscles ~/Downloads/z-anatomy-body.glb \
 *     --skin    ~/Downloads/athletic-body.glb \
 *     --out     web/assets/models/body.glb
 *
 * See ./README.md for where the sources come from and how to tune the alignment.
 */
import { existsSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { NodeIO } from "@gltf-transform/core";
import { ALL_EXTENSIONS, KHRDracoMeshCompression } from "@gltf-transform/extensions";
import {
  dedup,
  mergeDocuments,
  prune,
  quantize,
  simplify,
  textureCompress,
  unpartition,
  weld,
} from "@gltf-transform/functions";
import draco3d from "draco3dgltf";
import { MeshoptDecoder, MeshoptEncoder, MeshoptSimplifier } from "meshoptimizer";
import sharp from "sharp";

// The one list of muscle names, shared with the browser. Importing it rather than
// restating it here is the whole reason muscles.js exists as its own module: a mesh
// this script drops is a mesh the viewer can never light up, so the two must agree
// by construction rather than by review.
import { MUSCLE_ALIASES, resolveKey } from "../../web/assets/js/shared/muscle-viewer/muscles.js";

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(HERE, "../..");

const DEFAULTS = {
  out: "web/assets/models/body.glb",
  // The node name and material name the runtime looks for to tell skin from muscle.
  // viewer.js's isUnderSkinNode() walks parents looking for exactly this.
  skinNode: "skin",
  skinMaterial: "skin-material",
  // Target for the outer body after decimation. The pre-NOR-6 shell was 8k triangles
  // for a whole human and read as faceted Lego at the silhouette; 60k is smooth at
  // the sizes this viewer renders at without dominating the file.
  skinTris: 60000,
  textureSize: 1024,
  skinScale: 1,
  skinOffset: [0, 0, 0],
};

function parseArgs(argv) {
  const args = { ...DEFAULTS };
  for (let i = 0; i < argv.length; i += 2) {
    const flag = argv[i];
    const value = argv[i + 1];
    if (!flag.startsWith("--")) fail(`unexpected argument "${flag}"`);
    if (value === undefined) fail(`${flag} needs a value`);
    switch (flag) {
      case "--muscles":
      case "--skin":
      case "--out":
        args[flag.slice(2)] = value;
        break;
      case "--skin-scale":
        args.skinScale = Number(value);
        break;
      case "--skin-offset":
        args.skinOffset = value.split(",").map(Number);
        if (args.skinOffset.length !== 3 || args.skinOffset.some(Number.isNaN)) {
          fail("--skin-offset wants three numbers, e.g. 0,-0.02,0");
        }
        break;
      case "--skin-tris":
        args.skinTris = Number(value);
        break;
      case "--texture-size":
        args.textureSize = Number(value);
        break;
      default:
        fail(`unknown flag "${flag}"`);
    }
  }
  if (!args.muscles) fail("--muscles is required");
  if (!args.skin) fail("--skin is required");
  return args;
}

function fail(message) {
  console.error(`build-body: ${message}`);
  console.error("see tools/model/README.md for usage");
  process.exit(1);
}

async function createIO() {
  return new NodeIO()
    .registerExtensions(ALL_EXTENSIONS)
    .registerDependencies({
      // The Z-Anatomy source ships Draco-compressed, so reading it needs the decoder
      // even though nothing this script writes uses Draco.
      "draco3d.decoder": await draco3d.createDecoderModule(),
      "draco3d.encoder": await draco3d.createEncoderModule(),
      "meshopt.decoder": MeshoptDecoder,
      "meshopt.encoder": MeshoptEncoder,
    });
}

/**
 * Drops every mesh that isn't one of the 15 highlightable muscle groups.
 *
 * The Z-Anatomy atlas is the whole body — skeleton, organs, cartilage, teeth, facial
 * muscles, individual finger tendons. Well over 90% of it is geometry North will
 * never highlight and the browser would download for nothing.
 */
function keepOnlyMuscles(document) {
  const root = document.getRoot();
  const kept = new Map(); // muscle key -> mesh count
  let dropped = 0;

  for (const node of root.listNodes()) {
    const mesh = node.getMesh();
    if (!mesh) continue;
    // Match on the node name first and the mesh name second — the atlas labels
    // anatomy on whichever of the two the exporter happened to fill in.
    const key = resolveKey(node.getName()) || resolveKey(mesh.getName());
    if (key) {
      kept.set(key, (kept.get(key) || 0) + 1);
    } else {
      node.setMesh(null);
      dropped += 1;
    }
  }

  const missing = Object.keys(MUSCLE_ALIASES).filter((key) => !kept.has(key));
  if (missing.length > 0) {
    fail(
      `the muscle source has no mesh for: ${missing.join(", ")}.\n` +
        "  Either the source changed or muscles.js names anatomy this asset doesn't carry —\n" +
        "  fix one of the two before shipping, or those groups can never light up.",
    );
  }

  console.log(`  muscles: kept ${[...kept.values()].reduce((a, b) => a + b, 0)} meshes across ${kept.size} groups, dropped ${dropped}`);
  return kept;
}

/**
 * Tags the skin so the runtime can tell it apart, and fits it to the muscle figure.
 *
 * Alignment is the fragile part of this pipeline: the two meshes come from unrelated
 * sources with unrelated scales and origins. The automatic fit matches overall height
 * and centres the footprint, which is right when both assets are a standing human in
 * a comparable pose. When it isn't right, --skin-scale and --skin-offset are the
 * manual override, and ?muscleDebug=1 in the browser is how you see what you're doing.
 */
function prepareSkin(document, muscleBounds, args) {
  const root = document.getRoot();
  const scene = root.listScenes()[0];
  if (!scene) fail("the skin source has no scene");

  for (const material of root.listMaterials()) material.setName(args.skinMaterial);

  const bounds = boundsOf(document);
  const autoScale = (muscleBounds.max[1] - muscleBounds.min[1]) / (bounds.max[1] - bounds.min[1]);
  const scale = autoScale * args.skinScale;

  const centre = (axis) => ((bounds.min[axis] + bounds.max[axis]) / 2) * scale;
  const muscleCentre = (axis) => (muscleBounds.min[axis] + muscleBounds.max[axis]) / 2;

  // A wrapper node so the transform applies once, whatever the source's own hierarchy
  // looks like, and so there is a single node to carry the "skin" tag.
  const wrapper = document.createNode(args.skinNode);
  wrapper.setScale([scale, scale, scale]);
  wrapper.setTranslation([
    muscleCentre(0) - centre(0) + args.skinOffset[0],
    // Feet on the floor rather than centres aligned: a difference in leg length
    // should show up at the head, not push the model through the contact shadow.
    muscleBounds.min[1] - bounds.min[1] * scale + args.skinOffset[1],
    muscleCentre(2) - centre(2) + args.skinOffset[2],
  ]);

  for (const node of scene.listChildren()) {
    scene.removeChild(node);
    wrapper.addChild(node);
  }
  scene.addChild(wrapper);

  console.log(`  skin: scaled ${scale.toFixed(4)}x, translated [${wrapper.getTranslation().map((n) => n.toFixed(3)).join(", ")}]`);
  return wrapper;
}

function boundsOf(document) {
  const min = [Infinity, Infinity, Infinity];
  const max = [-Infinity, -Infinity, -Infinity];
  for (const mesh of document.getRoot().listMeshes()) {
    for (const primitive of mesh.listPrimitives()) {
      const position = primitive.getAttribute("POSITION");
      if (!position) continue;
      for (let axis = 0; axis < 3; axis += 1) {
        min[axis] = Math.min(min[axis], position.getMin([])[axis]);
        max[axis] = Math.max(max[axis], position.getMax([])[axis]);
      }
    }
  }
  return { min, max };
}

function triangleCount(document) {
  let total = 0;
  for (const mesh of document.getRoot().listMeshes()) {
    for (const primitive of mesh.listPrimitives()) {
      const indices = primitive.getIndices();
      const position = primitive.getAttribute("POSITION");
      total += indices ? indices.getCount() / 3 : (position ? position.getCount() / 3 : 0);
    }
  }
  return Math.round(total);
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const io = await createIO();

  for (const path of [args.muscles, args.skin]) {
    if (!existsSync(path)) fail(`no such file: ${path}`);
  }

  console.log("reading sources");
  const muscleDoc = await io.read(args.muscles);
  const skinDoc = await io.read(args.skin);
  console.log(`  muscles: ${triangleCount(muscleDoc).toLocaleString()} tris`);
  console.log(`  skin:    ${triangleCount(skinDoc).toLocaleString()} tris`);

  console.log("extracting muscle groups");
  keepOnlyMuscles(muscleDoc);
  await muscleDoc.transform(prune());

  console.log("decimating skin");
  await skinDoc.transform(weld());
  const skinTris = triangleCount(skinDoc);
  if (skinTris > args.skinTris) {
    await skinDoc.transform(
      simplify({ simplifier: MeshoptSimplifier, ratio: args.skinTris / skinTris, error: 0.001 }),
    );
    console.log(`  ${skinTris.toLocaleString()} -> ${triangleCount(skinDoc).toLocaleString()} tris`);
  } else {
    console.log(`  ${skinTris.toLocaleString()} tris, already under the ${args.skinTris.toLocaleString()} target`);
  }

  console.log("aligning skin to muscles");
  prepareSkin(skinDoc, boundsOf(muscleDoc), args);

  console.log("merging");
  mergeDocuments(muscleDoc, skinDoc);
  // mergeDocuments appends the skin's scene as a second scene; fold its contents into
  // the one scene the loader will render, keeping the wrapper node (and its tag).
  const root = muscleDoc.getRoot();
  const [primaryScene, ...extraScenes] = root.listScenes();
  for (const scene of extraScenes) {
    for (const node of scene.listChildren()) {
      scene.removeChild(node);
      primaryScene.addChild(node);
    }
    scene.dispose();
  }
  root.setDefaultScene(primaryScene);

  if (!primaryScene.listChildren().some((node) => node.getName() === args.skinNode)) {
    fail(`the merged scene has no "${args.skinNode}" node — the runtime would render every muscle uncovered`);
  }

  console.log("compressing");
  // The atlas ships Draco-compressed. Decode has already happened; drop the
  // extension so the written GLB is Meshopt-only (the runtime does not vendor
  // a Draco decoder).
  for (const ext of muscleDoc.getRoot().listExtensionsUsed()) {
    if (ext.extensionName === KHRDracoMeshCompression.EXTENSION_NAME) ext.dispose();
  }
  await muscleDoc.transform(
    dedup(),
    prune(),
    textureCompress({ encoder: sharp, targetFormat: "jpeg", resize: [args.textureSize, args.textureSize] }),
    weld(),
    quantize(),
    // mergeDocuments leaves the skin's buffer beside the muscle one; a GLB
    // may only carry a single buffer.
    unpartition(),
  );
  const outPath = resolve(REPO_ROOT, args.out);
  await mkdir(dirname(outPath), { recursive: true });
  await writeFile(outPath, await io.writeBinary(muscleDoc));

  const bytes = (await readFile(outPath)).byteLength;
  console.log(`\nwrote ${args.out}`);
  console.log(`  ${triangleCount(muscleDoc).toLocaleString()} tris, ${(bytes / 1e6).toFixed(2)} MB`);
  if (bytes > 6e6) {
    console.warn("  WARNING: over the 6 MB budget — drop --texture-size to 512 or --skin-tris lower");
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
