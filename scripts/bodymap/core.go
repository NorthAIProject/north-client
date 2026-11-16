package main

import (
	"log"
	"math"
)

// The muscles do not cover a body: there are none on the head, hands, feet or
// knees, and tendon fills the gaps between them. Drawn inside an outline they
// leave holes that show whatever is behind the figure. The core is the body
// under them: the skin sunk to just below the muscle beneath it, and left at
// the surface where there is none. Clients draw it opaque, so the figure reads
// as one solid body with the muscles lying on it.
//
// It ships as one more shape key on the shell, "core", so it costs a sparse
// accessor instead of a second mesh, and iOS builds it from BodyShell.bin the
// way it builds the skin. Clients draw the shell's geometry twice: once as the
// outline with the measurement keys, once as the core with only this key at 1.

const (
	coreKey    = "core"
	coreReach  = 0.045  // metres under the skin a muscle is looked for
	coreStep   = 0.002  // metres between samples along the way in
	coreNear   = 0.003  // a sample this close to a muscle has reached it
	coreUnder  = 0.004  // metres the core sits below the muscle it found
	coreSkin   = 0.0015 // metres the core sits below bare skin
	coreSmooth = 3      // passes that ease the step between the two
)

// coreTarget is the shape key that sinks the skin to the core. The muscles
// must be in the skin's space, and field must be the skin's distance field.
func coreTarget(skin mesh, normals []vec3, muscles []triangle, field distanceField) morphTarget {
	idx := newTriangleIndex(muscles, coreNear*2)
	depth := make([]float64, len(skin.verts))
	var covered int
	for i, v := range skin.verts {
		depth[i] = coreSkin
		n := normals[i]
		deepest := 0.0
		for s := 0.0; s <= coreReach; s += coreStep {
			p := v.sub(n.scale(s))
			// Past the middle of a thin part (a wrist, a finger) the way in
			// is the way out the far side, and what lies beyond is another
			// part of the body. The field is coarse, so only a clear rise
			// counts.
			at := field.at(p)
			deepest = math.Min(deepest, at)
			if at > deepest+fitVoxel {
				break
			}
			if _, _, ok := idx.closest(p, coreNear); ok {
				depth[i] = s + coreUnder
				covered++
				break
			}
		}
	}

	// Ease the step at a muscle's edge, without ever rising above the depth a
	// vertex needs: a core above its muscle would cover it.
	neighbours := make([][]int, len(skin.verts))
	for _, t := range skin.tris {
		for k := range 3 {
			a, b := t[k], t[(k+1)%3]
			neighbours[a] = append(neighbours[a], b)
		}
	}
	for range coreSmooth {
		next := make([]float64, len(depth))
		for i, ns := range neighbours {
			sum := depth[i]
			for _, j := range ns {
				sum += depth[j]
			}
			next[i] = math.Max(depth[i], sum/float64(len(ns)+1))
		}
		depth = next
	}

	target := morphTarget{name: coreKey, deltas: map[int]vec3{}}
	for i, d := range depth {
		target.deltas[i] = normals[i].scale(-d)
	}
	log.Printf("core: %.0f%% of the skin has muscle under it", 100*float64(covered)/float64(len(skin.verts)))
	return target
}
