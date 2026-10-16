// Command bodymap builds the body figure every client draws: the muscles of
// the anatomical atlas, one mesh per muscle key, inside the body's skin.
//
// tools/model/body-source.glb carries an outer skin and the Z-Anatomy muscle
// meshes fitted inside it (see tools/model/README.md). This tool refits the
// atlas to the skin, fits each arm on its own, tucks any muscle still poking
// out back under the skin, and groups the atlas meshes by muscle key. Clients
// draw the skin's back faces opaque and dark and its front faces translucent,
// so the muscles show through a body.
//
// Outputs, all committed:
//
//   - -glb:    the meshes for three.js — "shell" and one node per key —
//     quantized (KHR_mesh_quantization), which the vendored loader reads
//     without a decoder.
//   - -figure: a generated Go file with front and back SVG outlines, the
//     fallback when a browser has no WebGL. Skipped when empty.
//   - -usdz:   the same meshes for RealityKit on iOS (needs macOS's usdcat and
//     usdzip, both in /usr/bin). Skipped when empty.
//   - -ibl:    the web viewer's studio light rig as an equirectangular PNG,
//     for RealityKit's image-based lighting. Skipped when empty.
//   - -baseline: a generated Go file with the shell's own girths at waist,
//     hips and chest, as modelled and at each shape key's full strength.
//     internal/bodymap reads a person's measurements against it, so the
//     server's shape weights and the mesh's shape keys always agree.
//
// Run it from the repository root (task bodymap does):
//
//	go run ./scripts/bodymap \
//	  -usdz ../north-ios-app/khepri/khepri/Resources/BodyMap.usdz \
//	  -ibl ../north-ios-app/khepri/khepri/Resources/BodyStudio.png
//
// scripts/bodymap/preview is a page for looking at the result.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/NorthAIProject/north-client/internal/bodymap"
)

func main() {
	in := flag.String("in", "tools/model/body-source.glb", "source GLB with the skin and the muscle atlas")
	musclesJS := flag.String("muscles", "web/assets/js/shared/muscle-viewer/muscles.js", "file holding MUSCLE_ALIASES")
	outGLB := flag.String("glb", "web/assets/models/body-map.glb", "meshes for the web viewer")
	outFigure := flag.String("figure", "web/shared/muscleviewer/figure_gen.go", "generated SVG outlines; skipped when empty")
	outUSDZ := flag.String("usdz", "", "meshes for iOS; skipped when empty")
	outIBL := flag.String("ibl", "", "studio light rig for iOS, as an equirectangular PNG; skipped when empty")
	outBaseline := flag.String("baseline", "internal/bodymap/baseline_gen.go", "generated Go file with the shell's girths, which the server reads shapes against")
	subdivide := flag.Bool("subdivide", true, "smooth the skin with one step of Loop subdivision (four times the triangles)")
	flag.Parse()

	aliases, err := readAliases(*musclesJS)
	if err != nil {
		log.Fatal(err)
	}
	src, err := readGLB(*in)
	if err != nil {
		log.Fatalf("read %s: %v", *in, err)
	}

	skinTris, muscleTris, muscleKeys, err := split(src, aliases)
	if err != nil {
		log.Fatal(err)
	}
	skin := weld(skinTris, 1e-5)
	if *subdivide {
		skin = loopSubdivide(skin)
	}
	lo, hi := skin.bounds()
	log.Printf("skin: %d vertices, %d triangles, bounds %.3f..%.3f", len(skin.verts), len(skin.tris), lo, hi)

	fit, field := fitAtlas(skin, muscleTris)
	for i, t := range muscleTris {
		muscleTris[i] = triangle{fit.apply(t[0]), fit.apply(t[1]), fit.apply(t[2])}
	}
	reportFit(field, muscleTris)
	fitArms(field, muscleTris, muscleKeys)
	reportFit(field, muscleTris)
	pushInside(field, skin, muscleTris, muscleKeys)

	// Stand the figure on the origin: soles at y = 0, centred in x and z, so
	// every client can frame it without measuring it first. The muscles share
	// the skin's space, so they move with it.
	centre := vec3{(lo[0] + hi[0]) / 2, lo[1], (lo[2] + hi[2]) / 2}
	for i, v := range skin.verts {
		skin.verts[i] = v.sub(centre)
	}
	for i, t := range muscleTris {
		muscleTris[i] = triangle{t[0].sub(centre), t[1].sub(centre), t[2].sub(centre)}
	}

	muscles := muscleRegions(muscleTris, muscleKeys)
	if err = checkCoverage(muscles); err != nil {
		log.Fatal(err)
	}
	shell := skinRegion(skin)
	morphs, baseline, err := buildShellMorphs(skin, shell.normals)
	if err != nil {
		log.Fatal(err)
	}
	shell.morphs = morphs
	log.Printf("shell girths over height: waist %.3f→%.2f, hips %.3f→%.2f, chest %.3f→%.2f",
		baseline.Waist.From, baseline.Waist.To, baseline.Hips.From, baseline.Hips.To, baseline.Chest.From, baseline.Chest.To)
	regions := append([]region{shell}, muscles...)
	for _, r := range regions {
		log.Printf("  %-11s %6d vertices %6d triangles", r.key, len(r.verts), len(r.tris))
	}
	if err := writeRegionsGLB(*outGLB, regions); err != nil {
		log.Fatal(err)
	}
	if info, err := os.Stat(*outGLB); err == nil {
		log.Printf("wrote %s (%.0f KB)", *outGLB, float64(info.Size())/1024)
	}
	if err := writeBaseline(*outBaseline, baseline); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", *outBaseline)
	if *outFigure != "" {
		if err := writeFigure(*outFigure, shell, muscles); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *outFigure)
	}
	if *outUSDZ != "" {
		if err := writeUSDZ(*outUSDZ, regions); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *outUSDZ)
	}
	if *outIBL != "" {
		if err := writeStudioPNG(*outIBL, 512, 256); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *outIBL)
	}
}

// split separates the skin (everything under the node named "skin") from the
// atlas meshes that resolve to a muscle key. Bones, bursae, tendon sheaths and
// anything unnamed are dropped, and logged, so the figure's parts list can be
// read off a run.
func split(src *glbFile, aliases aliasTable) (skin, muscles []triangle, keys []string, err error) {
	var dropped []string
	for _, node := range src.meshNodes() {
		tris, err := src.triangles(node.mesh, node.world)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("node %q: %w", node.name, err)
		}
		if underSkin(node) {
			skin = append(skin, tris...)
			continue
		}
		muscle, ok := aliases.resolve(node.name)
		if !ok || !isMuscle(node.name) {
			dropped = append(dropped, node.name)
			continue
		}
		key, ok := bodymap.RegionOf(muscle)
		if !ok {
			return nil, nil, nil, fmt.Errorf("muscle key %q is neither in bodymap.Regions nor in bodymap.Fold", muscle)
		}
		for _, t := range orientOutward(tris) {
			muscles = append(muscles, t)
			keys = append(keys, key)
		}
	}
	if len(skin) == 0 {
		return nil, nil, nil, fmt.Errorf("no node named %q in the source", "skin")
	}
	sort.Strings(dropped)
	log.Printf("dropped %d non-muscle nodes: %s", len(dropped), strings.Join(dropped, ", "))
	return skin, muscles, keys, nil
}

func underSkin(n meshNode) bool {
	if n.name == "skin" {
		return true
	}
	for _, a := range n.ancestors {
		if a == "skin" {
			return true
		}
	}
	return false
}

// reportFit says how the fitted atlas sits in the skin. The muscles are drawn
// now, so a vertex outside the skin is a muscle poking through the body.
func reportFit(field distanceField, muscles []triangle) {
	var outside, total int
	var farthest float64
	for _, t := range muscles {
		for _, p := range t {
			total++
			if d := field.at(p); d > 0 {
				outside++
				farthest = math.Max(farthest, d)
			}
		}
	}
	log.Printf("atlas fit: %.1f%% of muscle vertices outside the skin, farthest %.0f mm", 100*float64(outside)/float64(total), farthest*1000)
}

// checkCoverage fails when the figure and internal/bodymap disagree: every
// muscle mesh must be a bodymap.Regions key, and every bodymap.Regions key
// must have a mesh. The server folds the remaining muscle keys into these.
func checkCoverage(muscles []region) error {
	have := map[string]bool{}
	for _, r := range muscles {
		have[r.key] = true
	}
	var problems []string
	for key := range have {
		if !bodymap.IsRegion(key) {
			problems = append(problems, fmt.Sprintf("%q has a mesh but is not in bodymap.Regions", key))
		}
	}
	for _, key := range bodymap.Regions {
		if !have[key] {
			problems = append(problems, fmt.Sprintf("%q is in bodymap.Regions but the atlas has no muscle for it; add it to bodymap.Fold", key))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("figure and internal/bodymap disagree:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}
