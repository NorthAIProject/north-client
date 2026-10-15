package main

import (
	"sort"
	"strings"
)

// shellRegion names the skin mesh. Clients draw its back faces opaque and
// dark and its front faces translucent, so the muscles show through a body.
const shellRegion = "shell"

// isMuscle rejects atlas meshes that resolve to a key by containment but are
// not muscle. "Ulna" is in "ulnar head of flexor carpi ulnaris" and "Scapula"
// in "scapular spinal part of deltoid"; bursae and tendon sheaths match their
// muscle's name. Hidden under the skin they were harmless; drawn, they would
// be lumps.
func isMuscle(name string) bool {
	n := normalizeName(name)
	if n == "ulna" || n == "scapula" {
		return false
	}
	return !strings.Contains(n, "bursa") && !strings.Contains(n, "tendon sheath")
}

// orientOutward flips a closed mesh's triangles when their winding faces in:
// the clients cull back faces, so a muscle wound the wrong way would vanish.
// The sign of the summed tetrahedron volumes about the origin says which way
// a closed surface faces.
func orientOutward(tris []triangle) []triangle {
	var volume float64
	for _, t := range tris {
		volume += t[0].dot(t[1].cross(t[2]))
	}
	if volume >= 0 {
		return tris
	}
	for i, t := range tris {
		tris[i] = triangle{t[0], t[2], t[1]}
	}
	return tris
}

// region is one named mesh of the figure, indexed on its own.
type region struct {
	key     string
	verts   []vec3
	normals []vec3
	tris    [][3]int
}

// skinRegion is the whole skin as one mesh, with smooth normals.
func skinRegion(skin mesh) region {
	return region{key: shellRegion, verts: skin.verts, normals: skin.normals(), tris: skin.tris}
}

// muscleRegions groups the fitted atlas triangles by muscle key, welds each
// group (the atlas arrives as soup, some of it split along every edge) and
// gives it area-weighted normals: one smooth mesh per key, named for tapping.
func muscleRegions(tris []triangle, keys []string) []region {
	byKey := map[string][]triangle{}
	for i, t := range tris {
		byKey[keys[i]] = append(byKey[keys[i]], t)
	}
	names := make([]string, 0, len(byKey))
	for k := range byKey {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]region, 0, len(names))
	for _, k := range names {
		m := weld(byKey[k], 1e-5)
		out = append(out, region{key: k, verts: m.verts, normals: m.normals(), tris: m.tris})
	}
	return out
}
