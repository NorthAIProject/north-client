// Command bodymap builds the body figure every client draws: the skin of
// web/assets/models/body.glb, cut into one region per muscle key.
//
// body.glb carries two things: an outer skin, and the Z-Anatomy muscle meshes
// fitted inside it (see tools/model/README.md). This tool keeps only the skin,
// smooths it with one step of Loop subdivision, and gives each skin triangle
// the key of the nearest muscle surface underneath — so "quads" is the patch of
// skin over the quadriceps, not the muscle itself. Skin with no muscle within
// -radius (head, hands, feet, knees) is the "base" region.
//
// Outputs, all committed:
//
//   - -glb:    the region meshes for three.js, one node per key, plain float32
//     and uint16 so no decoder is needed.
//   - -figure: a generated Go file with front and back SVG outlines of the same
//     regions, the fallback when a browser has no WebGL.
//   - -usdz:   the same meshes for RealityKit on iOS (needs macOS's usdcat and
//     usdzip, both in /usr/bin).
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
	"os"
	"sort"
	"strings"

	"github.com/NorthAIProject/north-client/internal/bodymap"
)

func main() {
	in := flag.String("in", "tools/model/body-source.glb", "source GLB with the skin and the muscle atlas")
	musclesJS := flag.String("muscles", "web/assets/js/shared/muscle-viewer/muscles.js", "file holding MUSCLE_ALIASES")
	outGLB := flag.String("glb", "web/assets/models/body-map.glb", "region meshes for the web viewer")
	outFigure := flag.String("figure", "web/shared/muscleviewer/figure_gen.go", "generated SVG outlines")
	outUSDZ := flag.String("usdz", "", "region meshes for iOS; skipped when empty")
	radius := flag.Float64("radius", 0.05, "how far under the skin a muscle may sit and still claim it, in metres")
	debugSVG := flag.String("debug-svg", "", "write a flat-shaded front/back preview here")
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
	skin := loopSubdivide(weld(skinTris, 1e-5))
	lo, hi := skin.bounds()
	log.Printf("skin: %d vertices, %d triangles, bounds %.3f..%.3f", len(skin.verts), len(skin.tris), lo, hi)

	fit := fitAtlas(skin, muscleTris)
	for i, t := range muscleTris {
		muscleTris[i] = triangle{fit.apply(t[0]), fit.apply(t[1]), fit.apply(t[2])}
	}
	idx := newMuscleIndex(muscleTris, muscleKeys, *radius)
	vertexLabels := labelVertices(skin, idx, *radius)
	smoothVertices(vertexLabels, skin.vertexNeighbours(), 4)
	skin, labels := splitByLabels(skin, vertexLabels)
	dropIslands(labels, skin.adjacency(), 60)

	// Stand the figure on the origin: soles at y = 0, centred in x and z, so
	// every client can frame it without measuring it first.
	lo, hi = skin.bounds()
	centre := vec3{(lo[0] + hi[0]) / 2, lo[1], (lo[2] + hi[2]) / 2}
	for i, v := range skin.verts {
		skin.verts[i] = v.sub(centre)
	}

	if *debugSVG != "" {
		if err := writeDebugSVG(*debugSVG, skin, labels); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(strings.TrimSuffix(*debugSVG, ".svg")+"-flat.svg", []byte(figureSVG(skin, labels)), 0o644); err != nil {
			log.Fatal(err)
		}
	}
	regions := buildRegions(skin, labels)
	if err := checkCoverage(regions); err != nil {
		log.Fatal(err)
	}
	if err := writeRegionsGLB(*outGLB, regions); err != nil {
		log.Fatal(err)
	}
	if info, err := os.Stat(*outGLB); err == nil {
		log.Printf("wrote %s (%.0f KB)", *outGLB, float64(info.Size())/1024)
	}
	if err := writeFigure(*outFigure, skin, labels); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", *outFigure)
	if *outUSDZ != "" {
		if err := writeUSDZ(*outUSDZ, regions); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *outUSDZ)
	}
}

// split separates the skin (everything under the node named "skin") from the
// atlas meshes that resolve to a muscle key. Bones and anything unnamed are
// dropped.
func split(src *glbFile, aliases aliasTable) (skin, muscles []triangle, keys []string, err error) {
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
		if !ok || strings.Contains(strings.ToLower(node.name), "bursa") {
			// Bursae match by containment ("bursa of biceps femoris" → biceps)
			// and are not muscle anyway.
			continue
		}
		key, ok := bodymap.RegionOf(muscle)
		if !ok {
			return nil, nil, nil, fmt.Errorf("muscle key %q is neither in bodymap.Regions nor in bodymap.Fold", muscle)
		}
		for _, t := range tris {
			muscles = append(muscles, t)
			keys = append(keys, key)
		}
	}
	if len(skin) == 0 {
		return nil, nil, nil, fmt.Errorf("no node named %q in the source", "skin")
	}
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

// region is one key's share of the skin, re-indexed on its own.
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
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// checkCoverage fails when the figure and internal/bodymap disagree: every
// region key must be listed in bodymap.Regions, and every bodymap.Regions key
// must have skin. The server folds the remaining muscle keys into these.
func checkCoverage(regions []region) error {
	have := map[string]bool{}
	for _, r := range regions {
		have[r.key] = true
		log.Printf("  %-11s %6d triangles", r.key, len(r.tris))
	}
	var problems []string
	for key := range have {
		if key != baseRegion && !bodymap.IsRegion(key) {
			problems = append(problems, fmt.Sprintf("%q has skin but is not in bodymap.Regions", key))
		}
	}
	for _, key := range bodymap.Regions {
		if !have[key] {
			problems = append(problems, fmt.Sprintf("%q is in bodymap.Regions but no skin reached it; add it to bodymap.Fold", key))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("figure and internal/bodymap disagree:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}
