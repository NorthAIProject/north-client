package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
)

// shellMagic opens BodyShell.bin; shellVersion changes with its layout.
const (
	shellMagic   = "KSHL"
	shellVersion = 1
)

// writeShellBin writes the skin and its shape keys for iOS, which builds the
// skin mesh itself instead of reading it from BodyMap.usdz. RealityKit applies
// USD blend shapes only through a skeleton, and gives no way to read the
// deformed points back to test them; a mesh the app builds from known points
// is moved exactly as the web's morph targets move it.
//
// The layout is little-endian with no padding:
//
//	"KSHL", version     uint32
//	vertices, triangles, targets uint32
//	points    [vertices][3]float32, metres
//	normals   [vertices][3]float32
//	indices   [triangles][3]uint32
//	per target, in the glTF's order:
//	  name length uint32, name UTF-8
//	  count       uint32
//	  vertex      [count]uint32, ascending
//	  delta       [count][3]float32, metres at full strength
func writeShellBin(path string, shell region) error {
	data, err := shellBin(shell)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func shellBin(shell region) ([]byte, error) {
	if len(shell.normals) != len(shell.verts) {
		return nil, fmt.Errorf("shell: %d normals for %d vertices", len(shell.normals), len(shell.verts))
	}
	var b bytes.Buffer
	put := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	putVec := func(v vec3) { put([3]float32{float32(v[0]), float32(v[1]), float32(v[2])}) }

	b.WriteString(shellMagic)
	put(uint32(shellVersion))
	put(uint32(len(shell.verts)))
	put(uint32(len(shell.tris)))
	put(uint32(len(shell.morphs)))
	for _, v := range shell.verts {
		putVec(v)
	}
	for _, n := range shell.normals {
		putVec(n)
	}
	for _, t := range shell.tris {
		put([3]uint32{uint32(t[0]), uint32(t[1]), uint32(t[2])})
	}
	for _, t := range shell.morphs {
		idx := make([]int, 0, len(t.deltas))
		for i := range t.deltas {
			if i < 0 || i >= len(shell.verts) {
				return nil, fmt.Errorf("shell: %s moves vertex %d of %d", t.name, i, len(shell.verts))
			}
			idx = append(idx, i)
		}
		sort.Ints(idx)
		put(uint32(len(t.name)))
		b.WriteString(t.name)
		put(uint32(len(idx)))
		for _, i := range idx {
			put(uint32(i))
		}
		for _, i := range idx {
			putVec(t.deltas[i])
		}
	}
	return b.Bytes(), nil
}
