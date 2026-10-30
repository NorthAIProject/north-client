# Model tools

Builds `tools/model/body-source.glb`: the anatomical atlas placed inside an outer
skin. It is not served. `task bodymap` (`go run ./scripts/bodymap`) turns it into the
figure every client draws (`scripts/bodymap/body-map.glb`, which the web app serves,
and the iOS `BodyMap.usdz`); see `scripts/bodymap/README.md`.

**The raw sources are not on disk any more.** `body-source.glb` is committed and is
the only input the figure needs; the scripts here are the record of how it was made
and are only needed to rebuild it from fresh downloads.

This is a build-time tool. It is not part of the Go build, it is not deployed, and
nothing in the application imports it. It exists because `body.glb` is generated from
two unrelated source assets and that recipe has to live somewhere it can be re-run.

## Why two sources

The viewer needs one asset that does two incompatible jobs:

- **Muscles** must be individually named meshes, one per anatomical structure, so
  `setLoads("quads")` can find them. Only an anatomical atlas gives you that.
- **The body** must look like a person. Anatomical atlases look like a cadaver.

So the muscle set comes from an atlas, the outer body comes from a character model,
and this script fits the second around the first.

## Usage

```sh
cd tools/model
npm install

# If the skin is the Sketchfab T-pose base mesh, fold the arms first:
node prepare-skin.mjs \
  --in  ~/Downloads/human_body_base_mesh_male.glb \
  --out /tmp/north-model-src/skin-arms-down.glb

node build-body.mjs \
  --muscles /tmp/north-model-src/z-anatomy-body.glb \
  --skin    /tmp/north-model-src/skin-arms-down.glb \
  --out     tools/model/body-source.glb
```

`--out` is relative to the repository root, not to `tools/model`.

| flag | default | what it does |
|---|---|---|
| `--muscles` | required | anatomical source; must contain the meshes named in `muscles.js` |
| `--skin` | required | outer body |
| `--out` | `tools/model/body-source.glb` | relative to the repository root |
| `--skin-scale` | `1` | multiplier *on top of* the automatic height fit |
| `--skin-offset` | `0,0,0` | manual nudge in muscle-space units, applied after the fit |
| `--skin-tris` | `60000` | decimation target for the outer body |
| `--texture-size` | `1024` | longest edge; drop to `512` if the file exceeds 6 MB |

## What the sources have to be

**Muscles.** [hpfrei/body-anatomy-3d-viewer](https://github.com/hpfrei/body-anatomy-3d-viewer)'s
`public/body.glb`, built on [Z-Anatomy](https://www.z-anatomy.com/) (CC BY-SA 4.0). The
script keeps only meshes whose names match `MUSCLE_ALIASES` in
`tools/model/muscles.js` — the same table `scripts/bodymap` uses to
group the figure's muscles — and hard-fails if any of the 15 groups has no matching mesh,
because a group with no geometry can never be highlighted and nothing else would tell you.

**Skin.** Any humanoid body mesh, subject to three requirements:

1. **Arms down at the sides.** The muscle atlas is arms-down. A T-pose skin cannot be
   reconciled with it without a rig, and the previous version of this asset worked
   around that by cropping the arms off at the shoulders — which is why the figure
   used to have hollow stumps where its deltoids should be.
2. **No albedo needed.** The runtime draws the skin as a translucent shell over the
   muscles and drops any baked colour map, UVs included.
3. **Roughly human proportions.** See alignment below.

## Alignment is the fragile part

The two sources have unrelated scales, origins and body proportions. The script fits
the skin to the muscles automatically — matches total height, centres the footprint,
puts the feet on the same floor — which is correct when both assets are a standing
human of ordinary proportions, and wrong otherwise.

Matching total height is also why this fit is off in a way that matters: the atlas
has no skull, so it gets stretched to the top of the head and every muscle sits about
a tenth of the body too high. `scripts/bodymap` refits the atlas to the skin itself,
so the figure is right even though this file is not. Check a change with the preview
page in `scripts/bodymap/preview/`.

The fit measures world-space bounds (`fit.mjs`), node transforms included, and the
build fails if the fitted skin is not within 10% of the muscle height. Fitting against
raw vertex bounds once shipped a skin ~170× too small, which drew nothing at all:
no skin, and therefore no glow either.

### Repairing a shipped file without the sources

When only the fit is wrong, you do not need the sources. `refit-skin.mjs` recomputes
the `skin` node's transform inside the built file and leaves everything else as it is:

```
node tools/model/refit-skin.mjs [--skin-scale 1.02] [--skin-offset 0,0,0]
```

## Budget

Target 2–6 MB, per NOR-6. The script warns above 6 MB. Textures dominate — reduce
`--texture-size` before `--skin-tris`, since silhouette smoothness is the thing this
whole exercise was about.

## Licensing

Z-Anatomy is CC BY-SA 4.0, and ShareAlike is viral: whatever the skin's own licence,
the **combined** asset ships as CC BY-SA 4.0. Attribution for every source lives in
the web app's site footer (north-web-app). Update it when you change a source.
