# body-map.glb

The body figure every client draws: the web viewer
(`web/assets/js/shared/muscle-viewer/viewer.js`) loads this file, and iOS loads the
same meshes as `BodyMap.usdz` (the muscles) and `BodyShell.bin` (the skin). All are written by `task bodymap` (`go run
./scripts/bodymap`), along with the flat no-WebGL figure
(`web/shared/muscleviewer/figure_gen.go`) and the iOS light rig (`BodyStudio.png`).
Do not edit any of them by hand; change the tool or its source and rebuild.

## What is in it

Under a root node called `body`: `shell`, the skin of a body, and then one node per
muscle key (`quads`, `delts`, `traps`…) holding that muscle's own geometry from the
anatomical atlas. Each node has its own mesh and a material of the same name, so a
client recolours a muscle by setting one material colour and resolves a tap by the
hit mesh's name.

The web viewer draws the shell as an outline only: clear where it faces the
camera, firm at the silhouette, so the muscles read as the body and the skin as
its edge. (It used to be drawn twice, an opaque dark inside and a grey
translucent front, which read as a second, grey person standing around the
muscles.) The shell is never a tap target.

The shell carries four shape keys (glTF morph targets, named in
`mesh.extras.targetNames`): `waist`, `hips`, `chest` and `overall`. Each is 0
(as modelled) to 1. The server works the weights out from a person's latest
progress check (`bodymap.Shape`, read against `bodymap.ShellBaseline`, which
this tool generates in `internal/bodymap/baseline_gen.go`), and the web
viewer applies them with `setShape`. Only the skin moves; the muscles stay
where they are, so a heavier body is the same muscles in a wider skin.

- `waist`, `hips` and `chest` push the torso's skin straight out from its
  axis, the same distance all round at a given height, so the muscle relief
  is not magnified into rolls. Each reaches about a tenth of the height up
  and down from its landmark and eases off smoothly, so neighbouring keys
  blend into one outline. The waist leans forward, the hips backward; the
  chest widens evenly.
- Each is solved, by measuring, to reach a fixed girth over height at full
  strength: waist 0.75, hips 0.80, chest 0.82. As modelled the shell measures
  about 0.46, 0.58 and 0.61. Girths are taken the way a tape takes them:
  around the convex hull of the torso's slice.
- `overall` moves the body out along its normals, at most 15 mm; the server
  caps the weight at 0.4. It leaves the hands and the head alone.
- Arms and hands never move with the bands: below the armpit each arm is its
  own piece of skin, found by connectivity, and no band reaches above the
  armpit. At extreme weights the waist's sides can reach the hanging arms.
- The keys are sparse int16 accessors on the positions' own grid, about
  0.1 MB in all.

iOS gets the skin separately, as `BodyShell.bin` (`-shell`; the layout is in
`scripts/bodymap/shell_bin.go`): float32 points, normals and indices, and
each shape key as sparse float32 deltas. `BodyMap.usdz` carries only the
muscles. The app builds the skin mesh from the file and recomputes its
points when the weights change, which is only when a check is saved.
RealityKit applies USD blend shapes only through a skeleton, and offers no
way to read the deformed points back, so a test could not show the keys
move the skin as the web's do; a mesh built from known points can be tested.
Like the web, iOS keeps the modelled normals under the keys.

The muscle keys are `internal/bodymap.Regions`; the tool fails if the file and that
list disagree. Muscles with no mesh of their own are folded server-side
(`bodymap.Fold`: rhomboids show on `traps`, serratus on `abs`), so clients only ever
receive region keys.

Positions are int16 and normals int8 (`KHR_mesh_quantization`, which the vendored
three.js loader reads natively); the root node's translation and scale map the grid
back to metres. About 1.5 MB with the shape keys; the tool refuses to write
more than 2 MB. The figure
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
and sides (`SIDES=side` for the profile, `SHAPE=0.5` or `SHAPE=waist:1,hips:0.4`
for a shaped figure). Use it to check a change.

## Licence

Muscles: [Z-Anatomy](https://www.z-anatomy.com/) via
[hpfrei/body-anatomy-3d-viewer](https://github.com/hpfrei/body-anatomy-3d-viewer),
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). Skin:
[Human Body Base Mesh Male](https://sketchfab.com/3d-models/human-body-base-mesh-male-3678451d8ccb435e833f8a10729c09f5)
by [ferrumiron6](https://sketchfab.com/ferrumiron6), CC BY 4.0. The combined figure
is CC BY-SA 4.0. Attribution lives in the site footer (`siteFooter()` in
`web/landing/sections.templ`) and in the iOS app's acknowledgements.
