package main

import (
	"bytes"
	"encoding/binary"
	"math"
)

// idleColour is the untrained-muscle grey-blue the clients start from; they
// recolour each region's material at runtime, so this only shows in tools
// that open the file directly.
var idleColour = []float64{0.56, 0.62, 0.70, 1}

// writeRegionsGLB writes one node, mesh and material per region, each named
// after its key, under a root node called "body". Float32 positions and
// normals and uint16 indices (uint32 past 65,535 vertices): bigger than
// quantized, but readable by every loader with no extension or decoder.
func writeRegionsGLB(path string, regions []region) error {
	doc := gltfDoc{
		Asset:   map[string]any{"version": "2.0", "generator": "north-client scripts/bodymap"},
		Scenes:  []gltfScene{{Name: "body-map", Nodes: []int{0}}},
		Nodes:   []gltfNode{{Name: "body"}},
		Buffers: []gltfBuffer{{}},
	}
	var bin bytes.Buffer

	addView := func(data []byte, target int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		doc.BufferViews = append(doc.BufferViews, gltfBufferView{Buffer: 0, ByteOffset: bin.Len(), ByteLength: len(data), Target: target})
		bin.Write(data)
		return len(doc.BufferViews) - 1
	}
	addVec3 := func(vs []vec3, withBounds bool) int {
		var b bytes.Buffer
		lo := []float64{math.Inf(1), math.Inf(1), math.Inf(1)}
		hi := []float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
		for _, v := range vs {
			for k := range 3 {
				f := float32(v[k])
				_ = binary.Write(&b, binary.LittleEndian, f)
				lo[k] = math.Min(lo[k], float64(f))
				hi[k] = math.Max(hi[k], float64(f))
			}
		}
		view := addView(b.Bytes(), 34962)
		acc := gltfAccessor{BufferView: &view, ComponentType: componentFloat, Count: len(vs), Type: "VEC3"}
		if withBounds {
			acc.Min, acc.Max = lo, hi
		}
		doc.Accessors = append(doc.Accessors, acc)
		return len(doc.Accessors) - 1
	}
	addIndices := func(tris [][3]int, vertexCount int) int {
		var b bytes.Buffer
		componentType := componentUnsignedShort
		if vertexCount > math.MaxUint16 {
			componentType = componentUnsignedInt
		}
		for _, t := range tris {
			for _, i := range t {
				if componentType == componentUnsignedShort {
					_ = binary.Write(&b, binary.LittleEndian, uint16(i))
				} else {
					_ = binary.Write(&b, binary.LittleEndian, uint32(i))
				}
			}
		}
		view := addView(b.Bytes(), 34963)
		doc.Accessors = append(doc.Accessors, gltfAccessor{BufferView: &view, ComponentType: componentType, Count: len(tris) * 3, Type: "SCALAR"})
		return len(doc.Accessors) - 1
	}

	for _, r := range regions {
		position := addVec3(r.verts, true)
		normal := addVec3(r.normals, false)
		indices := addIndices(r.tris, len(r.verts))
		material := len(doc.Materials)
		doc.Materials = append(doc.Materials, map[string]any{
			"name": r.key,
			"pbrMetallicRoughness": map[string]any{
				"baseColorFactor": idleColour,
				"metallicFactor":  0,
				"roughnessFactor": 0.65,
			},
		})
		mesh := len(doc.Meshes)
		doc.Meshes = append(doc.Meshes, gltfMesh{Name: r.key, Primitives: []gltfPrimitive{{
			Attributes: map[string]int{"POSITION": position, "NORMAL": normal},
			Indices:    &indices,
			Material:   &material,
		}}})
		doc.Nodes = append(doc.Nodes, gltfNode{Name: r.key, Mesh: &mesh})
		doc.Nodes[0].Children = append(doc.Nodes[0].Children, len(doc.Nodes)-1)
	}
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	doc.Buffers[0].ByteLength = bin.Len()
	return writeGLB(path, doc, bin.Bytes())
}
