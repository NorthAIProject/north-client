# body.glb

The figure rendered by the muscle viewer
(`web/assets/js/shared/muscle-viewer/viewer.js`). Two layers, both real assets — no
procedural or primitive geometry.

Built by `tools/model/build-body.mjs`. That script and its README are the authority
on how to regenerate this file, what the sources have to look like, and how to fix
the alignment when it goes wrong. This file records only what the current asset *is*.

## Layers

**Muscles** — [hpfrei/body-anatomy-3d-viewer](https://github.com/hpfrei/body-anatomy-3d-viewer)
(`public/body.glb`), built on anatomical data from [Z-Anatomy](https://www.z-anatomy.com/).
Licensed [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). Pruned from
the full atlas (826 meshes: skeleton, organs, cartilage, teeth, facial muscles,
hand/foot detail) down to the 116 meshes matching North's 15 highlightable muscle
groups — every anatomical head and both sides, named as in the source (e.g.
`Vastus lateralis muscle`, `Vastus lateralis muscle.001`). ~76,000 triangles.

**Skin** — the outer body. Opaque since NOR-6; the muscles are sealed inside it and
only surface as a glow where an exercise works them.

> **Current state (NOR-6, in progress).** The skin is still the interim asset:
> [Human Body Base Mesh Male](https://sketchfab.com/3d-models/human-body-base-mesh-male-3678451d8ccb435e833f8a10729c09f5)
> by [ferrumiron6](https://sketchfab.com/ferrumiron6), CC BY 4.0. It is a rigless
> T-pose with no UVs and no textures, so the pipeline crops its arms off at the
> shoulders (`|x| > 0.32m`) to avoid a pose clash, and the runtime falls back to flat
> shading. That is why the figure currently has hollow stumps at the deltoids and
> reads as clay rather than skin. Replacing it with an arms-down, textured export is
> the remaining half of NOR-6 — see `tools/model/README.md` for the requirements.

Per CC BY-SA's ShareAlike clause the combined asset is licensed **CC BY-SA 4.0**,
whatever the skin's own licence. Attribution for every source lives in the site
footer (`web/landing/sections.templ`, `siteFooter()`).

## Compression

`EXT_meshopt_compression` + `KHR_mesh_quantization`. Meshopt rather than Draco — see
the header of `web/assets/js/vendor/three-gltf-loader.module.js` for why.

## Contract with the runtime

Two things in this file are load-bearing, and `build-body.mjs` asserts both:

- the outer body hangs under a node named **`skin`**, with its material named
  `skin-material`. `isUnderSkinNode()` in `viewer.js` walks parents looking for
  exactly that name to decide what is body and what is muscle.
- every muscle mesh is named as in `web/assets/js/shared/muscle-viewer/muscles.js`.
  A mesh the viewer can't resolve to a muscle key is hidden, not drawn.
