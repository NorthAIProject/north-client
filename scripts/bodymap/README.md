# body-map.glb

The body figure every client draws: the web viewer in `north-web-app` loads this
file, and iOS loads the same meshes as `BodyMap.usdz` (the muscles) and
`BodyShell.bin` (the skin). All are written by `task bodymap` (`go run
./scripts/bodymap`), along with the iOS light rig (`BodyStudio.png`) and
`internal/bodymap/baseline_gen.go`. Do not edit any of them by hand; change the
tool or its source and rebuild.

The web app does not read this repository. After regenerating, copy
`scripts/bodymap/body-map.glb` to `north-web-app/static/assets/models/` by hand.

## What is in it

Under a root node called `body`: `shell`, the skin of a body, and then one node per
muscle key (`quads`, `delts`, `traps`…) holding that muscle's own geometry from the
anatomical atlas. Each node has its own mesh and a material of the same name, so a
client recolours a muscle by setting one material colour and resolves a tap by the
hit mesh's name.

The clients draw the shell's geometry twice. Once as the body under the
muscles, opaque and pale, with the `core` shape key at 1: that key sinks the
skin to just below the muscle under it and leaves it at the surface where there
is none (the head, hands, feet and knees, and the tendon between muscles), so
the figure is one solid body with the muscles lying on it and nothing shows
through. And once as an outline: clear where it faces the camera, firm at the
silhouette, with the measurement keys below. (An outline alone left holes
wherever there was no muscle. Before that the skin was an opaque dark inside
and a grey translucent front, which read as a second, grey person standing
around the muscles; the body is under the muscles now, not around them.) The
shell is never a tap target.

Besides `core`, the shell carries four shape keys (glTF morph targets, named in
`mesh.extras.targetNames`): `waist`, `hips`, `chest` and `overall`. Each is 0
(as modelled) to 1. They move the outline only; the core stays at `core` = 1
and nothing else. The server works the weights out from a person's latest
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

Positions are int16 and normals int8 (`KHR_mesh_quantization`, which the
three.js loader reads natively); the root node's translation and scale map the grid
back to metres. About 1.8 MB with the shape keys; the tool refuses to write
more than 2 MB. The figure
stands on the origin (soles at y = 0, centred in x and z) and faces +z.

## How it is made

`scripts/bodymap` reads `tools/model/body-source.glb` — the anatomical atlas placed
inside an outer skin, built once by `tools/model/build-body.mjs` from sources that
are no longer on disk — and:

1. keeps the atlas meshes that name a muscle in `MUSCLE_ALIASES`
   (`tools/model/muscles.js`), dropping bones, bursae and
   tendon sheaths by name;
2. welds the skin and smooths it with one step of Loop subdivision;
3. refits the atlas to the skin (the source fit stretched the headless atlas to
   the full height of the body). The fit is scored two ways: muscle poking out
   of the skin costs, and so does skin with no muscle near it. Without the
   second the atlas is free to shrink into the torso, which is what it once
   did: the calves ended at mid-shin and the lower legs were bare;
4. fits each arm on its own about its shoulder, then to its sleeve
   (`sleeve.go`): this skin's upper arms are about twice the atlas's and its
   forearms hardly thicker, so at each height the arm's muscles are centred on
   the skin's arm and stretched out to it;
5. tucks any muscle vertex still outside the skin 2 mm under it, and draws the
   rest out towards the skin (`hug`): the skin is a fuller body than the atlas,
   and muscle left centimetres under it reads as a thin body inside a bigger one;
6. drops the long tendons below the wrists and ankles, which never line up
   with this skin's fingers and toes;
7. works out the `core` shape key (`core.go`), groups the atlas triangles by
   key, welds each group and writes the outputs.

The run logs the fit, how much muscle sits outside the skin after each step,
and how much of the skin has muscle under it; a jump in any of them means the
fit needs looking at before anything is committed. Look at the result from the
front, the back and the side before committing it: the numbers did not catch
the bare lower legs.

To look at a change, copy `body-map.glb` into the sibling `north-web-app` checkout's
`static/assets/models/` and open the body figure there: the viewer is part of the
web app now.

## Licence

Muscles: [Z-Anatomy](https://www.z-anatomy.com/) via
[hpfrei/body-anatomy-3d-viewer](https://github.com/hpfrei/body-anatomy-3d-viewer),
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). Skin:
[Human Body Base Mesh Male](https://sketchfab.com/3d-models/human-body-base-mesh-male-3678451d8ccb435e833f8a10729c09f5)
by [ferrumiron6](https://sketchfab.com/ferrumiron6), CC BY 4.0. The combined figure
is CC BY-SA 4.0. Attribution lives in the web app's site footer (north-web-app)
and in the iOS app's acknowledgements.
