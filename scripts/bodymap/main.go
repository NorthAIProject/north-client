// Command bodymap builds the body figure every client draws: the muscles of
// the anatomical atlas, one mesh per muscle key, inside the body's skin.
//
// tools/model/body-source.glb carries an outer skin and the Z-Anatomy muscle
// meshes fitted inside it (see tools/model/README.md). This tool refits the
// atlas to the skin, groups its meshes by muscle key, and splits the skin into
// "shell" (skin over muscle) and "base" (skin with no muscle within -radius:
// head, hands, feet, joints). Clients draw the skin's back faces opaque and
// dark and its front faces translucent, so the muscles show through a body.
//
// Outputs, all committed:
//
//   - -glb:    the meshes for three.js — shell, base, then one node per key —
//     plain float32 and uint16 so no decoder is needed.
//   - -figure: a generated Go file with front and back SVG outlines, the
//     fallback when a browser has no WebGL. Skipped when empty.
//   - -usdz:   the same meshes for RealityKit on iOS (needs macOS's usdcat and
//     usdzip, both in /usr/bin). Skipped when empty.
//
// Run it from the repository root:
//
//	go run ./scripts/bodymap \
//	  -usdz ../north-ios-app/khepri/khepri/Resources/BodyMap.usdz
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
	radius := flag.Float64("radius", 0.05, "how far under the skin a muscle may sit and still claim it, in metres")
	subdivide := flag.Bool("subdivide", false, "smooth the skin with one step of Loop subdivision (four times the triangles)")
	debugSVG := flag.String("debug-svg", "", "write a flat-shaded front/back preview of the skin labels here")
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
	pushInside(field, skin, muscleTris)

	idx := newMuscleIndex(muscleTris, muscleKeys, *radius)
	vertexLabels := labelVertices(skin, idx, *radius)
	smoothVertices(vertexLabels, skin.vertexNeighbours(), 4)
	skin, labels := splitByLabels(skin, vertexLabels)
	dropIslands(labels, skin.adjacency(), 60)

	// Stand the figure on the origin: soles at y = 0, centred in x and z, so
	// every client can frame it without measuring it first. The muscles share
	// the skin's space, so they move with it.
	lo, hi = skin.bounds()
	centre := vec3{(lo[0] + hi[0]) / 2, lo[1], (lo[2] + hi[2]) / 2}
	for i, v := range skin.verts {
		skin.verts[i] = v.sub(centre)
	}
	for i, t := range muscleTris {
		muscleTris[i] = triangle{t[0].sub(centre), t[1].sub(centre), t[2].sub(centre)}
	}

	if *debugSVG != "" {
		if err := writeDebugSVG(*debugSVG, skin, labels); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(strings.TrimSuffix(*debugSVG, ".svg")+"-flat.svg", []byte(figureSVG(skin, labels)), 0o644); err != nil {
			log.Fatal(err)
		}
	}

	muscles := muscleRegions(muscleTris, muscleKeys)
	if err := checkCoverage(muscles); err != nil {
		log.Fatal(err)
	}
	regions := append(buildRegions(skin, collapseLabels(labels)), muscles...)
	for _, r := range regions {
		log.Printf("  %-11s %6d vertices %6d triangles", r.key, len(r.verts), len(r.tris))
	}
	if err := writeRegionsGLB(*outGLB, regions); err != nil {
		log.Fatal(err)
	}
	if info, err := os.Stat(*outGLB); err == nil {
		log.Printf("wrote %s (%.0f KB)", *outGLB, float64(info.Size())/1024)
	}
	if *outFigure != "" {
		if err := writeFigure(*outFigure, skin, labels); err != nil {
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

// region is one named mesh of the figure, indexed on its own.
type region struct {
	key     string
	verts   []vec3
	normals []vec3
	tris    [][3]int
}

// buildRegions splits the skin by label. A vertex on a border is copied into
// each region that uses it with the same normal, so the shading stays smooth
// across the seam.
func buildRegions(skin mesh, labels []string) []region {
	normals := skin.normals()
	byKey := map[string]*region{}
	remap := map[string]map[int]int{}
	for i, t := range skin.tris {
		key := labels[i]
		r, ok := byKey[key]
		if !ok {
			r = &region{key: key}
			byKey[key] = r
			remap[key] = map[int]int{}
		}
		var face [3]int
		for k, v := range t {
			at, ok := remap[key][v]
			if !ok {
				at = len(r.verts)
				remap[key][v] = at
				r.verts = append(r.verts, skin.verts[v])
				r.normals = append(r.normals, normals[v])
			}
			face[k] = at
		}
		r.tris = append(r.tris, face)
	}
	out := make([]region, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, *r)
	}
	// Shell first, then base: the clients tell them apart by name, but a tool
	// that opens the file directly sees the body before the parts.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].key == shellRegion) != (out[j].key == shellRegion) {
			return out[i].key == shellRegion
		}
		return out[i].key < out[j].key
	})
	return out
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
