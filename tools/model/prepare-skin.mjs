#!/usr/bin/env node
/**
 * Prepares the Sketchfab T-pose base mesh so build-body.mjs can wear it.
 *
 * The source is arms-out and untextured. Placeholder capsules exist in the
 * viewer because of both. This script folds the arms down around the shoulders
 * and stamps a skin-tone albedo so the runtime treats it as a real body.
 *
 * Input:  ~/Downloads/human_body_base_mesh_male.glb (or --in)
 * Output: /tmp/north-model-src/skin-arms-down.glb   (or --out)
 */
import { mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";

import { NodeIO } from "@gltf-transform/core";
import { ALL_EXTENSIONS } from "@gltf-transform/extensions";
import draco3d from "draco3dgltf";
import { MeshoptDecoder } from "meshoptimizer";
import sharp from "sharp";

function parseArgs(argv) {
  const args = {
    in: resolve(process.env.HOME, "Downloads/human_body_base_mesh_male.glb"),
    out: "/tmp/north-model-src/skin-arms-down.glb",
  };
  for (let i = 0; i < argv.length; i += 2) {
    const flag = argv[i];
    const value = argv[i + 1];
    if (flag === "--in") args.in = value;
    else if (flag === "--out") args.out = value;
    else {
      console.error(`unknown flag ${flag}`);
      process.exit(1);
    }
  }
  return args;
}

function foldArms(document) {
  // Eyes in this export are a separate mesh at a different scale and blow the
  // bounding box. The body mesh is the figure.
  for (const node of document.getRoot().listNodes()) {
    const mesh = node.getMesh();
    if (!mesh) continue;
    if (/eye/i.test(node.getName()) || /eye/i.test(mesh.getName())) {
      node.setMesh(null);
    }
  }

  const body = document
    .getRoot()
    .listMeshes()
    .find((mesh) => /body/i.test(mesh.getName())) || document.getRoot().listMeshes()[0];
  if (!body) throw new Error("no body mesh");

  const prim = body.listPrimitives()[0];
  const pos = prim.getAttribute("POSITION");

  let minY = Infinity;
  let maxY = -Infinity;
  let minX = Infinity;
  let maxX = -Infinity;
  for (let i = 0; i < pos.getCount(); i++) {
    const p = pos.getElement(i, []);
    minY = Math.min(minY, p[1]);
    maxY = Math.max(maxY, p[1]);
    minX = Math.min(minX, p[0]);
    maxX = Math.max(maxX, p[0]);
  }
  const height = maxY - minY;
  // Torso is the narrow band under the head; arms in this T-pose sit around
  // 0.75 of height and reach |x| ≈ 0.5 of height.
  const shoulderY = minY + height * 0.78;
  const shoulderX = height * 0.12;
  const armGateX = height * 0.14;
  const armGateY = minY + height * 0.55;

  let folded = 0;
  for (let i = 0; i < pos.getCount(); i++) {
    const p = pos.getElement(i, []);
    if (Math.abs(p[0]) < armGateX || p[1] < armGateY) continue;
    const side = Math.sign(p[0]);
    const px = p[0] - side * shoulderX;
    const py = p[1] - shoulderY;
    // Rotate 90° around Z so +X / -X hang down toward -Y.
    const theta = side > 0 ? -Math.PI / 2 : Math.PI / 2;
    const cos = Math.cos(theta);
    const sin = Math.sin(theta);
    const rx = px * cos - py * sin;
    const ry = px * sin + py * cos;
    p[0] = rx + side * shoulderX;
    p[1] = ry + shoulderY;
    pos.setElement(i, p);
    folded += 1;
  }
  pos.setArray(pos.getArray());
  console.log(`  folded ${folded} arm vertices, shoulder x=${shoulderX.toFixed(1)} y=${shoulderY.toFixed(1)}`);
}

async function stampAlbedo(document) {
  // Subtle noise so a 1024 map does not read as a flat plastic fill.
  const size = 1024;
  const raw = Buffer.alloc(size * size * 3);
  for (let i = 0; i < size * size; i++) {
    const n = (Math.sin(i * 12.9898) * 43758.5453) % 1;
    const d = (n < 0 ? n + 1 : n) * 14 - 7;
    raw[i * 3] = Math.max(0, Math.min(255, 185 + d));
    raw[i * 3 + 1] = Math.max(0, Math.min(255, 137 + d * 0.8));
    raw[i * 3 + 2] = Math.max(0, Math.min(255, 99 + d * 0.6));
  }
  const jpeg = await sharp(raw, { raw: { width: size, height: size, channels: 3 } })
    .jpeg({ quality: 78 })
    .toBuffer();

  const texture = document.createTexture("skin-albedo").setImage(jpeg).setMimeType("image/jpeg");
  for (const material of document.getRoot().listMaterials()) {
    material.setBaseColorTexture(texture);
    material.setRoughnessFactor(0.72);
    material.setMetallicFactor(0);
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const io = new NodeIO()
    .registerExtensions(ALL_EXTENSIONS)
    .registerDependencies({
      "draco3d.decoder": await draco3d.createDecoderModule(),
      "meshopt.decoder": MeshoptDecoder,
    });

  console.log("reading", args.in);
  const doc = await io.read(args.in);
  console.log("folding T-pose arms down");
  foldArms(doc);
  console.log("stamping skin albedo");
  await stampAlbedo(doc);

  await mkdir(dirname(args.out), { recursive: true });
  await io.write(args.out, doc);
  console.log("wrote", args.out);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
