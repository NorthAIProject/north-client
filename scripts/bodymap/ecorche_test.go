package main

import (
	"reflect"
	"testing"
)

func TestIsMuscle(t *testing.T) {
	for name, want := range map[string]bool{
		"Ulna.002":                                 false,
		"Scapula.003":                              false,
		"Subdeltoid bursa":                         false,
		"Tendon sheath of flexor carpi radialis":   false,
		"Ulnar head of flexor carpi ulnaris":       true,
		"Scapular spinal part of deltoid muscle.3": true,
	} {
		if got := isMuscle(name); got != want {
			t.Errorf("isMuscle(%q) = %v, want %v", name, got, want)
		}
	}
}

// A tetrahedron wound inward is flipped to face out.
func TestOrientOutward(t *testing.T) {
	a, b, c, d := vec3{1, 1, 1}, vec3{1, -1, -1}, vec3{-1, 1, -1}, vec3{-1, -1, 1}
	outward := []triangle{{a, b, c}, {a, c, d}, {a, d, b}, {b, d, c}}
	volume := func(tris []triangle) float64 {
		var v float64
		for _, t := range tris {
			v += t[0].dot(t[1].cross(t[2]))
		}
		return v
	}
	if volume(outward) <= 0 {
		t.Fatal("test tetrahedron is not wound outward")
	}
	inward := make([]triangle, len(outward))
	for i, tri := range outward {
		inward[i] = triangle{tri[0], tri[2], tri[1]}
	}
	if got := orientOutward(inward); volume(got) <= 0 {
		t.Error("inward winding was not flipped")
	}
	if got := orientOutward(outward); volume(got) <= 0 {
		t.Error("outward winding was flipped")
	}
}

// Soup for two keys becomes one welded mesh per key, in key order.
func TestMuscleRegions(t *testing.T) {
	a, b, c, d := vec3{0, 0, 0}, vec3{1, 0, 0}, vec3{0, 1, 0}, vec3{1, 1, 0}
	tris := []triangle{{a, b, c}, {b, d, c}, {a, c, d}}
	keys := []string{"quads", "quads", "abs"}
	got := muscleRegions(tris, keys)
	if len(got) != 2 || got[0].key != "abs" || got[1].key != "quads" {
		t.Fatalf("regions = %+v, want abs then quads", got)
	}
	if len(got[1].verts) != 4 || len(got[1].tris) != 2 {
		t.Errorf("quads: %d vertices, %d triangles; want the shared edge welded (4, 2)", len(got[1].verts), len(got[1].tris))
	}
	if len(got[1].normals) != len(got[1].verts) {
		t.Errorf("normals %d != vertices %d", len(got[1].normals), len(got[1].verts))
	}
	if !reflect.DeepEqual(got[0].verts, []vec3{a, c, d}) {
		t.Errorf("abs verts = %v", got[0].verts)
	}
}
