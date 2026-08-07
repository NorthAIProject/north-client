# body.glb

3D model for the muscle viewer (`web/assets/js/shared/muscle-viewer/viewer.js`,
NOR-6, extracted from the landing page and generalized for production use in
NOR-8). Two layers, both real assets — no procedural/primitive geometry.

## Sources

**Muscles** — [hpfrei/body-anatomy-3d-viewer](https://github.com/hpfrei/body-anatomy-3d-viewer)
(`public/body.glb`), built on anatomical data from [Z-Anatomy](https://www.z-anatomy.com/).
Licensed [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). Pruned from the
full atlas (826 meshes: skeleton, organs, cartilage, teeth, facial muscles, hand/foot
detail) down to the 116 meshes matching North's 15 highlightable muscle groups — every
anatomical head and both sides, named as in the source (e.g. `Vastus lateralis muscle`,
`Vastus lateralis muscle.001`).

**Skin** — [Human Body Base Mesh Male](https://sketchfab.com/3d-models/human-body-base-mesh-male-3678451d8ccb435e833f8a10729c09f5)
by [ferrumiron6](https://sketchfab.com/ferrumiron6) on Sketchfab. Licensed
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). Source mesh is a static
T-pose with no rig — arms can't be repositioned to match the muscle data's arms-down
pose without hand-sculpting (not available in this pipeline), so the skin is cropped at
the shoulders (world-space |x| > 0.32m) and kept only for head/torso/legs. Arms show the
Z-Anatomy muscle geometry directly, uncovered.

Per CC BY-SA's ShareAlike clause, the combined asset is licensed **CC BY-SA 4.0**.
Attribution for both sources lives in the site footer (`web/landing/sections.templ`,
`siteFooter()`).

## How it was built

No Blender involved — this is a scripted pipeline (`@gltf-transform/core` +
`@gltf-transform/functions`, Node), because the Z-Anatomy source already ships
individually named, anatomically separated meshes; no manual segmentation was needed.

1. **Extract muscles**: read the source `body.glb` (Draco-compressed), keep only nodes
   whose name (after stripping Blender's `.NNN` duplicate suffix) matches one of the 15
   muscle groups' known anatomical names, drop everything else, weld/dedupe.
2. **Crop skin**: read the skin `.glb`, transform each vertex by its node's world
   matrix, drop any triangle with a vertex beyond `|x| > 0.32` (removes the T-pose
   arms), drop the `Eyes` sub-mesh, weld.
3. **Merge**: combine both into one document (`mergeDocuments`), tag the skin's
   top-level node `"skin"` and its material `"skin-material"` so the runtime loader can
   treat it separately (always translucent, never colored by muscle load) without
   depending on traversal order.
4. **Compress**: `gltf-transform meshopt --level high` (Meshopt, not Draco — see
   `web/assets/js/vendor/three-gltf-loader.module.js`'s header for why) + prune +
   `KHR_mesh_quantization`.

Final size: ~1MB (well under the 2–3MB target). No textures, no materials baked in —
every material is assigned at runtime by `muscle-viewer.js`'s `loadFigure()`.

## Regenerating

The extraction/crop/merge scripts aren't checked into this repo (they're a one-off
build step, not application code). If the source assets change or the skin fit needs
adjusting, the pipeline above is reproducible in a scratch Node project with
`@gltf-transform/core`, `@gltf-transform/functions`, `@gltf-transform/extensions`,
`draco3dgltf` (to read the Draco-compressed source), and the `gltf-transform` CLI.
