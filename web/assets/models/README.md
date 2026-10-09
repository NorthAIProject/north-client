# body-map.glb

The body figure every client draws: the web viewer
(`web/assets/js/shared/muscle-viewer/viewer.js`) loads this file, and iOS loads the
same meshes as `BodyMap.usdz`. Both are written by `go run ./scripts/bodymap`, along
with the flat no-WebGL figure (`web/shared/muscleviewer/figure_gen.go`). Do not edit
any of them by hand; change the tool or its source and rebuild.

## What is in it

One node per region under a root called `body`. Each region is a patch of skin named
after a muscle key (`quads`, `delts`, `traps`…), with its own mesh and its own
material of the same name, so a client recolours a region by setting one material
colour and resolves a tap by the hit mesh's name. `base` is skin with no muscle under
it: head, hands, feet, knees and elbows.

The regions are the keys in `internal/bodymap.Regions`; the tool fails if the file
and that list disagree. Muscles with no skin of their own are folded server-side
(`bodymap.Fold`: rhomboids show on `traps`, serratus on `abs`), so clients only ever
receive region keys.

Plain float32 positions and normals and uint16 indices, about 920 KB, no extensions:
any glTF loader reads it without a decoder. The figure stands on the origin (soles at
y = 0, centred in x and z) and faces +z.

## How it is made

`scripts/bodymap` reads `tools/model/body-source.glb` — the anatomical atlas fitted
inside an outer skin, built by `tools/model/build-body.mjs` — and:

1. refits the atlas to the skin (the source fit stretched the headless atlas to the
   full height of the body, about a tenth too tall);
2. welds the skin and smooths it with one step of Loop subdivision;
3. gives every skin triangle the key of the nearest muscle surface within 5 cm,
   then smooths the borders and drops specks;
4. splits the skin by key and writes the three outputs.

`-debug-svg <path>` writes flat-shaded front/back previews with every region in its
own colour, which is the quickest way to check a change.

## Licence

Muscles: [Z-Anatomy](https://www.z-anatomy.com/) via
[hpfrei/body-anatomy-3d-viewer](https://github.com/hpfrei/body-anatomy-3d-viewer),
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). Skin:
[Human Body Base Mesh Male](https://sketchfab.com/3d-models/human-body-base-mesh-male-3678451d8ccb435e833f8a10729c09f5)
by [ferrumiron6](https://sketchfab.com/ferrumiron6), CC BY 4.0. The regions are cut
from the atlas's geometry, so the figure is CC BY-SA 4.0 like the combined source.
Attribution lives in the site footer (`siteFooter()` in `web/landing/sections.templ`)
and in the iOS app's acknowledgements.
