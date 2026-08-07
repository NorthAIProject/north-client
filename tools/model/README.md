# Model tools

Builds `web/assets/models/body.glb`, the figure the muscle viewer renders.

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

node build-body.mjs \
  --muscles ~/Downloads/z-anatomy-body.glb \
  --skin    ~/Downloads/athletic-body.glb \
  --out     ../../web/assets/models/body.glb
```

| flag | default | what it does |
|---|---|---|
| `--muscles` | required | anatomical source; must contain the meshes named in `muscles.js` |
| `--skin` | required | outer body |
| `--out` | `web/assets/models/body.glb` | relative to the repository root |
| `--skin-scale` | `1` | multiplier *on top of* the automatic height fit |
| `--skin-offset` | `0,0,0` | manual nudge in muscle-space units, applied after the fit |
| `--skin-tris` | `60000` | decimation target for the outer body |
| `--texture-size` | `1024` | longest edge; drop to `512` if the file exceeds 6 MB |

## What the sources have to be

**Muscles.** [hpfrei/body-anatomy-3d-viewer](https://github.com/hpfrei/body-anatomy-3d-viewer)'s
`public/body.glb`, built on [Z-Anatomy](https://www.z-anatomy.com/) (CC BY-SA 4.0). The
script keeps only meshes whose names match `MUSCLE_ALIASES` in
`web/assets/js/shared/muscle-viewer/muscles.js` — the same table the browser uses to
decide what to light up — and hard-fails if any of the 15 groups has no matching mesh,
because a group with no geometry can never be highlighted and nothing else would tell you.

**Skin.** Any humanoid body mesh, subject to three requirements:

1. **Arms down at the sides.** The muscle atlas is arms-down. A T-pose skin cannot be
   reconciled with it without a rig, and the previous version of this asset worked
   around that by cropping the arms off at the shoulders — which is why the figure
   used to have hollow stumps where its deltoids should be.
2. **UVs and baked PBR textures.** Without them the runtime falls back to flat
   shading (`SKIN_FALLBACK` in `viewer.js`) and the body looks like clay. This is the
   main reason to prefer a Tripo AI / Meshy export over a free base mesh: generated
   models come textured, most free base meshes do not.
3. **Roughly human proportions.** See alignment below.

## Alignment is the fragile part

The two sources have unrelated scales, origins and body proportions. The script fits
the skin to the muscles automatically — matches total height, centres the footprint,
puts the feet on the same floor — which is correct when both assets are a standing
human of ordinary proportions, and wrong otherwise.

When it's wrong, muscles poke out through the skin. To check:

```
http://localhost:8090/?muscleDebug=1
```

That renders the skin at 25% opacity. Anything sticking out is a fit problem. Correct
it with `--skin-scale` and `--skin-offset` and rebuild — a slightly *larger* skin is
always the safe direction, since the glow shader only draws muscle that has skin in
front of it, and a muscle poking through the surface simply vanishes at that spot.

## Budget

Target 2–6 MB, per NOR-6. The script warns above 6 MB. Textures dominate — reduce
`--texture-size` before `--skin-tris`, since silhouette smoothness is the thing this
whole exercise was about.

## Licensing

Z-Anatomy is CC BY-SA 4.0, and ShareAlike is viral: whatever the skin's own licence,
the **combined** asset ships as CC BY-SA 4.0. Attribution for every source lives in
`siteFooter()` in `web/landing/sections.templ`. Update it when you change a source.
