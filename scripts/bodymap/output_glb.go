package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
)

// idleColour is the untrained-muscle grey-blue the clients start from; they
// recolour each mesh's material at runtime, so this only shows in tools that
// open the file directly. The shell's own colour is translucent for the same
// reason: so such a tool shows muscles inside a body, not a grey doll.
var (
	idleColour  = []float64{0.56, 0.62, 0.70, 1}
	shellColour = []float64{0.78, 0.82, 0.87, 0.3}
)

// glbBudget is the most body-map.glb may weigh. It is fetched on every page
// with the figure, and the Today page is the first thing a person opens.
// Raised from 1.5 MB for the shell's shape keys, which add about 0.3 MB.
const glbBudget = 2_000_000

// quantizer maps metres onto one int16 grid for the whole figure, so every
// mesh shares a step and seams between them stay exact. The root node's
// translation and scale undo it in the loader (KHR_mesh_quantization).
type quantizer struct {
	origin vec3
	step   float64
}

func newQuantizer(regions []region) quantizer {
	lo := vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, r := range regions {
		for _, v := range r.verts {
			for k := range 3 {
				lo[k], hi[k] = math.Min(lo[k], v[k]), math.Max(hi[k], v[k])
			}
		}
	}
	extent := max(hi[0]-lo[0], hi[1]-lo[1], hi[2]-lo[2])
	return quantizer{origin: lo, step: extent / (math.MaxInt16 - math.MinInt16 - 1)}
}

func (q quantizer) encode(v vec3) [3]int16 {
	var out [3]int16
	for k := range 3 {
		out[k] = int16(math.Round((v[k]-q.origin[k])/q.step) + math.MinInt16 + 1)
	}
	return out
}

func (q quantizer) decode(c [3]int16) vec3 {
	var out vec3
	for k := range 3 {
		out[k] = q.origin[k] + (float64(c[k])-math.MinInt16-1)*q.step
	}
	return out
}

// translation and scale are the root node transform that maps grid units
// back to metres.
func (q quantizer) translation() []float64 {
	t := q.decode([3]int16{0, 0, 0})
	return []float64{t[0], t[1], t[2]}
}

func (q quantizer) scale() []float64 { return []float64{q.step, q.step, q.step} }

// writeRegionsGLB writes one node, mesh and material per region, each named
// after its key, under a root node called "body" that carries the
// quantization transform. int16 positions and int8 normals halve the file
// against float32; three.js reads them without a decoder.
func writeRegionsGLB(path string, regions []region) error {
	q := newQuantizer(regions)
	doc := gltfDoc{
		Asset:              map[string]any{"version": "2.0", "generator": "north-client scripts/bodymap"},
		ExtensionsUsed:     []string{"KHR_mesh_quantization"},
		ExtensionsRequired: []string{"KHR_mesh_quantization"},
		Scenes:             []gltfScene{{Name: "body-map", Nodes: []int{0}}},
		Nodes:              []gltfNode{{Name: "body", Translation: q.translation(), Scale: q.scale()}},
		Buffers:            []gltfBuffer{{}},
	}
	var bin bytes.Buffer

	addView := func(data []byte, target, stride int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		doc.BufferViews = append(doc.BufferViews, gltfBufferView{Buffer: 0, ByteOffset: bin.Len(), ByteLength: len(data), ByteStride: stride, Target: target})
		bin.Write(data)
		return len(doc.BufferViews) - 1
	}
	addPositions := func(vs []vec3) int {
		var b bytes.Buffer
		lo := []float64{math.MaxInt16, math.MaxInt16, math.MaxInt16}
		hi := []float64{math.MinInt16, math.MinInt16, math.MinInt16}
		for _, v := range vs {
			c := q.encode(v)
			for k := range 3 {
				_ = binary.Write(&b, binary.LittleEndian, c[k])
				lo[k], hi[k] = math.Min(lo[k], float64(c[k])), math.Max(hi[k], float64(c[k]))
			}
			b.Write([]byte{0, 0}) // pad each element to four bytes
		}
		view := addView(b.Bytes(), 34962, 8)
		doc.Accessors = append(doc.Accessors, gltfAccessor{BufferView: &view, ComponentType: componentShort, Count: len(vs), Type: "VEC3", Min: lo, Max: hi})
		return len(doc.Accessors) - 1
	}
	addNormals := func(ns []vec3) int {
		var b bytes.Buffer
		for _, n := range ns {
			for k := range 3 {
				b.WriteByte(byte(int8(math.Round(n[k] * math.MaxInt8))))
			}
			b.WriteByte(0)
		}
		view := addView(b.Bytes(), 34962, 4)
		doc.Accessors = append(doc.Accessors, gltfAccessor{BufferView: &view, ComponentType: componentByte, Normalized: true, Count: len(ns), Type: "VEC3"})
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
		view := addView(b.Bytes(), 34963, 0)
		doc.Accessors = append(doc.Accessors, gltfAccessor{BufferView: &view, ComponentType: componentType, Count: len(tris) * 3, Type: "SCALAR"})
		return len(doc.Accessors) - 1
	}

	// addMorph writes a shape key as a sparse accessor: only the vertices it
	// moves, as int16 steps of the same grid the positions use, so the root
	// node's scale turns them into metres too (KHR_mesh_quantization allows
	// SHORT morph positions).
	addMorph := func(t morphTarget, vertexCount int) int {
		idx := make([]int, 0, len(t.deltas))
		for i := range t.deltas {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		var ib, vb bytes.Buffer
		lo := []float64{0, 0, 0}
		hi := []float64{0, 0, 0}
		for _, i := range idx {
			if vertexCount > math.MaxUint16 {
				_ = binary.Write(&ib, binary.LittleEndian, uint32(i))
			} else {
				_ = binary.Write(&ib, binary.LittleEndian, uint16(i))
			}
			d := t.deltas[i]
			for k := range 3 {
				c := int16(math.Round(d[k] / q.step))
				_ = binary.Write(&vb, binary.LittleEndian, c)
				lo[k], hi[k] = math.Min(lo[k], float64(c)), math.Max(hi[k], float64(c))
			}
		}
		indexType := componentUnsignedShort
		if vertexCount > math.MaxUint16 {
			indexType = componentUnsignedInt
		}
		iv := addView(ib.Bytes(), 0, 0)
		vv := addView(vb.Bytes(), 0, 0)
		doc.Accessors = append(doc.Accessors, gltfAccessor{
			ComponentType: componentShort, Count: vertexCount, Type: "VEC3", Min: lo, Max: hi,
			Sparse: map[string]any{
				"count":   len(idx),
				"indices": map[string]any{"bufferView": iv, "componentType": indexType},
				"values":  map[string]any{"bufferView": vv},
			},
		})
		return len(doc.Accessors) - 1
	}

	for _, r := range regions {
		position := addPositions(r.verts)
		normal := addNormals(r.normals)
		indices := addIndices(r.tris, len(r.verts))
		material := len(doc.Materials)
		pbr := map[string]any{"baseColorFactor": idleColour, "metallicFactor": 0, "roughnessFactor": 0.65}
		spec := map[string]any{"name": r.key, "pbrMetallicRoughness": pbr}
		if r.key == shellRegion {
			pbr["baseColorFactor"] = shellColour
			spec["alphaMode"] = "BLEND"
		}
		doc.Materials = append(doc.Materials, spec)
		prim := gltfPrimitive{
			Attributes: map[string]int{"POSITION": position, "NORMAL": normal},
			Indices:    &indices,
			Material:   &material,
		}
		m := gltfMesh{Name: r.key}
		for _, t := range r.morphs {
			prim.Targets = append(prim.Targets, map[string]int{"POSITION": addMorph(t, len(r.verts))})
			m.Weights = append(m.Weights, 0)
		}
		if len(r.morphs) > 0 {
			names := make([]string, len(r.morphs))
			for i, t := range r.morphs {
				names[i] = t.name
			}
			m.Extras = map[string]any{"targetNames": names}
		}
		m.Primitives = []gltfPrimitive{prim}
		mesh := len(doc.Meshes)
		doc.Meshes = append(doc.Meshes, m)
		doc.Nodes = append(doc.Nodes, gltfNode{Name: r.key, Mesh: &mesh})
		doc.Nodes[0].Children = append(doc.Nodes[0].Children, len(doc.Nodes)-1)
	}
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	doc.Buffers[0].ByteLength = bin.Len()
	if err := writeGLB(path, doc, bin.Bytes()); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > glbBudget {
		return fmt.Errorf("%s is %d bytes, over the %d budget", path, info.Size(), glbBudget)
	}
	return nil
}
