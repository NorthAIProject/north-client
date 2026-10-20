package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// The sidecar reads back as the shell it was written from: the layout the
// iOS reader (BodyShell.swift) expects, field for field.
func TestShellBinRoundTrip(t *testing.T) {
	shell := region{
		key:     shellRegion,
		verts:   []vec3{{0, 0, 0}, {0.1, 0, 0}, {0, 0.2, 0.05}},
		normals: []vec3{{0, 0, 1}, {0, 0, 1}, {0, 0.6, 0.8}},
		tris:    [][3]int{{0, 1, 2}},
		morphs: []morphTarget{
			{name: "waist", deltas: map[int]vec3{2: {0, 0, 0.01}, 0: {0.003, 0, 0}}},
			{name: "overall", deltas: map[int]vec3{}},
		},
	}
	data, err := shellBin(shell)
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(data)
	get := func(v any) {
		t.Helper()
		if err := binary.Read(r, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	magic := make([]byte, 4)
	if _, err := r.Read(magic); err != nil || string(magic) != shellMagic {
		t.Fatalf("magic = %q", magic)
	}
	var version, verts, tris, targets uint32
	get(&version)
	get(&verts)
	get(&tris)
	get(&targets)
	if version != shellVersion || verts != 3 || tris != 1 || targets != 2 {
		t.Fatalf("header = v%d %d verts %d tris %d targets", version, verts, tris, targets)
	}
	points := make([][3]float32, verts)
	normals := make([][3]float32, verts)
	get(points)
	get(normals)
	if points[2] != [3]float32{0, 0.2, 0.05} || normals[2] != [3]float32{0, 0.6, 0.8} {
		t.Errorf("point 2 = %v, normal 2 = %v", points[2], normals[2])
	}
	indices := make([][3]uint32, tris)
	get(indices)
	if indices[0] != [3]uint32{0, 1, 2} {
		t.Errorf("triangle = %v", indices[0])
	}

	var nameLen, count uint32
	get(&nameLen)
	name := make([]byte, nameLen)
	get(name)
	get(&count)
	if string(name) != "waist" || count != 2 {
		t.Fatalf("first target = %q with %d", name, count)
	}
	vertex := make([]uint32, count)
	delta := make([][3]float32, count)
	get(vertex)
	get(delta)
	// Ascending vertex order, deltas in the same order.
	if vertex[0] != 0 || vertex[1] != 2 || math.Abs(float64(delta[0][0])-0.003) > 1e-9 || math.Abs(float64(delta[1][2])-0.01) > 1e-9 {
		t.Errorf("waist = %v %v", vertex, delta)
	}
	get(&nameLen)
	name = make([]byte, nameLen)
	get(name)
	get(&count)
	if string(name) != "overall" || count != 0 {
		t.Errorf("second target = %q with %d", name, count)
	}
	if r.Len() != 0 {
		t.Errorf("%d bytes left over", r.Len())
	}
}

func TestShellBinRefusesStrayVertex(t *testing.T) {
	shell := region{
		verts:   []vec3{{0, 0, 0}},
		normals: []vec3{{0, 0, 1}},
		morphs:  []morphTarget{{name: "waist", deltas: map[int]vec3{5: {}}}},
	}
	if _, err := shellBin(shell); err == nil {
		t.Error("a delta for a vertex the shell does not have was written")
	}
}
