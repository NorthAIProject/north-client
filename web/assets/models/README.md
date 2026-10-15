# body-map.glb

The body figure every client draws: the web viewer
(`web/assets/js/shared/muscle-viewer/viewer.js`) loads this file, and iOS loads the
same meshes as `BodyMap.usdz`. Both are written by `task bodymap` (`go run
./scripts/bodymap`), along with the flat no-WebGL figure
(`web/shared/muscleviewer/figure_gen.go`) and the iOS light rig (`BodyStudio.png`).
Do not edit any of them by hand; change the tool or its source and rebuild.

## What is in it

Under a root node called `body`: `shell`, the skin of a body, and then one node per
muscle key (`quads`, `delts`, `traps`…) holding that muscle's own geometry from the
anatomical atlas. Each node has its own mesh and a material of the same name, so a
client recolours a muscle by setting one material colour and resolves a tap by the
hit mesh's name.

Clients draw the shell twice from the one geometry: its back faces opaque and dark,
which makes the inside of the body a cavity the muscles sit in, and its front faces
translucent, firmed up at the silhouette, so the body's outline reads with the
muscles showing through. The shell is never a tap target.

The muscle keys are `internal/bodymap.Regions`; the tool fails if the file and that
list disagree. Muscles with no mesh of their own are folded server-side
(`bodymap.Fold`: rhomboids show on `traps`, serratus on `abs`), so clients only ever
receive region keys.

Positions are int16 and normals int8 (`KHR_mesh_quantization`, which the vendored
three.js loader reads natively); the root node's translation and scale map the grid
back to metres. About 1.4 MB; the tool refuses to write more than 1.5 MB. The figure
stands on the origin (soles at y = 0, centred in x and z) and faces +z.

## How it is made

`scripts/bodymap` reads `tools/model/body-source.glb` — the anatomical atlas placed
inside an outer skin, built once by `tools/model/build-body.mjs` from sources that
are no longer on disk — and:

1. keeps the atlas meshes that name a muscle in `MUSCLE_ALIASES`
   (`web/assets/js/shared/muscle-viewer/muscles.js`), dropping bones, bursae and
   tendon sheaths by name;
2. welds the skin and smooths it with one step of Loop subdivision;
3. refits the atlas to the skin (the source fit stretched the headless atlas to
   the full height of the body), then fits each arm on its own about its shoulder,
   and tucks any muscle vertex still outside the skin 2 mm under it;
4. groups the atlas triangles by key, welds each group and writes the outputs.

The run logs how much muscle sits outside the skin after each step; a jump there
means the fit needs looking at before anything is committed.

`scripts/bodymap/preview/` is a page that draws the file with the real viewer and
nothing else around it, plus a headless-Chrome script for screenshots of both themes
and sides. Use it to check a change.

## Licence

Muscles: [Z-Anatomy](https://www.z-anatomy.com/) via
[hpfrei/body-anatomy-3d-viewer](https://github.com/hpfrei/body-anatomy-3d-viewer),
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). Skin:
[Human Body Base Mesh Male](https://sketchfab.com/3d-models/human-body-base-mesh-male-3678451d8ccb435e833f8a10729c09f5)
by [ferrumiron6](https://sketchfab.com/ferrumiron6), CC BY 4.0. The combined figure
is CC BY-SA 4.0. Attribution lives in the site footer (`siteFooter()` in
`web/landing/sections.templ`) and in the iOS app's acknowledgements.
