package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
)

// Just enough of glTF 2.0 to read body.glb's triangles in world space and to
// write the region meshes back out. Anything the source does not use (skins,
// animations, sparse accessors, extensions beyond KHR_mesh_quantization) is a
// hard error rather than a silent misread.

type gltfDoc struct {
	Asset              map[string]any   `json:"asset"`
	ExtensionsUsed     []string         `json:"extensionsUsed,omitempty"`
	ExtensionsRequired []string         `json:"extensionsRequired,omitempty"`
	Scene              int              `json:"scene"`
	Scenes             []gltfScene      `json:"scenes"`
	Nodes              []gltfNode       `json:"nodes"`
	Meshes             []gltfMesh       `json:"meshes"`
	Materials          []map[string]any `json:"materials,omitempty"`
	Accessors          []gltfAccessor   `json:"accessors"`
	BufferViews        []gltfBufferView `json:"bufferViews"`
	Buffers            []gltfBuffer     `json:"buffers"`
}

type gltfScene struct {
	Name  string `json:"name,omitempty"`
	Nodes []int  `json:"nodes"`
}

type gltfNode struct {
	Name        string    `json:"name,omitempty"`
	Mesh        *int      `json:"mesh,omitempty"`
	Children    []int     `json:"children,omitempty"`
	Translation []float64 `json:"translation,omitempty"`
	Rotation    []float64 `json:"rotation,omitempty"`
	Scale       []float64 `json:"scale,omitempty"`
	Matrix      []float64 `json:"matrix,omitempty"`
}

type gltfMesh struct {
	Name       string          `json:"name,omitempty"`
	Primitives []gltfPrimitive `json:"primitives"`
}

type gltfPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices,omitempty"`
	Material   *int           `json:"material,omitempty"`
	Mode       *int           `json:"mode,omitempty"`
}

type gltfAccessor struct {
	BufferView    *int      `json:"bufferView,omitempty"`
	ByteOffset    int       `json:"byteOffset,omitempty"`
	ComponentType int       `json:"componentType"`
	Normalized    bool      `json:"normalized,omitempty"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float64 `json:"min,omitempty"`
	Max           []float64 `json:"max,omitempty"`
	Sparse        any       `json:"sparse,omitempty"`
}

type gltfBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset,omitempty"`
	ByteLength int `json:"byteLength"`
	ByteStride int `json:"byteStride,omitempty"`
	Target     int `json:"target,omitempty"`
}

type gltfBuffer struct {
	ByteLength int `json:"byteLength"`
}

const (
	componentByte          = 5120
	componentUnsignedByte  = 5121
	componentShort         = 5122
	componentUnsignedShort = 5123
	componentUnsignedInt   = 5125
	componentFloat         = 5126
)

// glbFile is a parsed GLB: the JSON document and its single binary chunk.
type glbFile struct {
	doc gltfDoc
	bin []byte
}

func readGLB(path string) (*glbFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 20 || string(data[:4]) != "glTF" {
		return nil, errors.New("not a GLB file")
	}
	jsonLen := int(binary.LittleEndian.Uint32(data[12:16]))
	if string(data[16:20]) != "JSON" {
		return nil, errors.New("first GLB chunk is not JSON")
	}
	var f glbFile
	if err := json.Unmarshal(data[20:20+jsonLen], &f.doc); err != nil {
		return nil, fmt.Errorf("parse glTF JSON: %w", err)
	}
	rest := data[20+jsonLen:]
	if len(rest) >= 8 && string(rest[4:8]) == "BIN\x00" {
		binLen := int(binary.LittleEndian.Uint32(rest[:4]))
		f.bin = rest[8 : 8+binLen]
	}
	for _, ext := range f.doc.ExtensionsRequired {
		if ext != "KHR_mesh_quantization" {
			return nil, fmt.Errorf("unsupported required extension %s", ext)
		}
	}
	return &f, nil
}

func componentCount(kind string) (int, error) {
	switch kind {
	case "SCALAR":
		return 1, nil
	case "VEC2":
		return 2, nil
	case "VEC3":
		return 3, nil
	case "VEC4":
		return 4, nil
	}
	return 0, fmt.Errorf("unsupported accessor type %s", kind)
}

func componentSize(componentType int) (int, error) {
	switch componentType {
	case componentByte, componentUnsignedByte:
		return 1, nil
	case componentShort, componentUnsignedShort:
		return 2, nil
	case componentUnsignedInt, componentFloat:
		return 4, nil
	}
	return 0, fmt.Errorf("unsupported component type %d", componentType)
}

// floats decodes an accessor into float64 tuples, applying the normalization
// rules KHR_mesh_quantization relies on.
func (f *glbFile) floats(index int) ([][]float64, error) {
	acc := f.doc.Accessors[index]
	if acc.Sparse != nil || acc.BufferView == nil {
		return nil, fmt.Errorf("accessor %d: sparse or bufferless accessors are not supported", index)
	}
	n, err := componentCount(acc.Type)
	if err != nil {
		return nil, err
	}
	size, err := componentSize(acc.ComponentType)
	if err != nil {
		return nil, err
	}
	view := f.doc.BufferViews[*acc.BufferView]
	stride := view.ByteStride
	if stride == 0 {
		stride = n * size
	}
	base := view.ByteOffset + acc.ByteOffset
	out := make([][]float64, acc.Count)
	for i := range acc.Count {
		tuple := make([]float64, n)
		for c := range n {
			at := base + i*stride + c*size
			tuple[c] = readComponent(f.bin[at:], acc.ComponentType, acc.Normalized)
		}
		out[i] = tuple
	}
	return out, nil
}

func readComponent(b []byte, componentType int, normalized bool) float64 {
	switch componentType {
	case componentFloat:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
	case componentByte:
		v := float64(int8(b[0]))
		if normalized {
			return math.Max(v/127, -1)
		}
		return v
	case componentUnsignedByte:
		v := float64(b[0])
		if normalized {
			return v / 255
		}
		return v
	case componentShort:
		v := float64(int16(binary.LittleEndian.Uint16(b)))
		if normalized {
			return math.Max(v/32767, -1)
		}
		return v
	case componentUnsignedShort:
		v := float64(binary.LittleEndian.Uint16(b))
		if normalized {
			return v / 65535
		}
		return v
	case componentUnsignedInt:
		return float64(binary.LittleEndian.Uint32(b))
	}
	return 0
}

// triangles returns the world-space triangles of every primitive of a mesh.
func (f *glbFile) triangles(meshIndex int, world mat4) ([]triangle, error) {
	var out []triangle
	for _, prim := range f.doc.Meshes[meshIndex].Primitives {
		if prim.Mode != nil && *prim.Mode != 4 {
			return nil, fmt.Errorf("mesh %d: only triangle lists are supported", meshIndex)
		}
		posIndex, ok := prim.Attributes["POSITION"]
		if !ok {
			continue
		}
		positions, err := f.floats(posIndex)
		if err != nil {
			return nil, err
		}
		var indices []int
		if prim.Indices != nil {
			raw, err := f.floats(*prim.Indices)
			if err != nil {
				return nil, err
			}
			for _, v := range raw {
				indices = append(indices, int(v[0]))
			}
		} else {
			for i := range positions {
				indices = append(indices, i)
			}
		}
		for i := 0; i+2 < len(indices); i += 3 {
			out = append(out, triangle{
				world.apply(vec3FromSlice(positions[indices[i]])),
				world.apply(vec3FromSlice(positions[indices[i+1]])),
				world.apply(vec3FromSlice(positions[indices[i+2]])),
			})
		}
	}
	return out, nil
}

// meshNode is a node with a mesh, its world transform, and the names of every
// ancestor, so the caller can tell the skin's subtree from the muscles.
type meshNode struct {
	name      string
	mesh      int
	world     mat4
	ancestors []string
}

func (f *glbFile) meshNodes() []meshNode {
	var out []meshNode
	var walk func(index int, parent mat4, ancestors []string)
	walk = func(index int, parent mat4, ancestors []string) {
		node := f.doc.Nodes[index]
		world := parent.mul(localMatrix(node))
		if node.Mesh != nil {
			out = append(out, meshNode{name: node.Name, mesh: *node.Mesh, world: world, ancestors: ancestors})
		}
		next := append(append([]string{}, ancestors...), node.Name)
		for _, child := range node.Children {
			walk(child, world, next)
		}
	}
	for _, root := range f.doc.Scenes[f.doc.Scene].Nodes {
		walk(root, identity(), nil)
	}
	return out
}

func localMatrix(n gltfNode) mat4 {
	if len(n.Matrix) == 16 {
		var m mat4
		copy(m[:], n.Matrix)
		return m
	}
	t := vec3{}
	if len(n.Translation) == 3 {
		t = vec3{n.Translation[0], n.Translation[1], n.Translation[2]}
	}
	r := [4]float64{0, 0, 0, 1}
	if len(n.Rotation) == 4 {
		copy(r[:], n.Rotation)
	}
	s := vec3{1, 1, 1}
	if len(n.Scale) == 3 {
		s = vec3{n.Scale[0], n.Scale[1], n.Scale[2]}
	}
	return compose(t, r, s)
}

// writeGLB writes doc and bin as a GLB, padding both chunks to four bytes.
func writeGLB(path string, doc gltfDoc, bin []byte) error {
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, ' ')
	}
	for len(bin)%4 != 0 {
		bin = append(bin, 0)
	}
	var buf bytes.Buffer
	total := 12 + 8 + len(jsonBytes) + 8 + len(bin)
	buf.WriteString("glTF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(total))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(jsonBytes)))
	buf.WriteString("JSON")
	buf.Write(jsonBytes)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(bin)))
	buf.WriteString("BIN\x00")
	buf.Write(bin)
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
