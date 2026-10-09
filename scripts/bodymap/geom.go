package main

import "math"

type vec3 [3]float64

func vec3FromSlice(s []float64) vec3 { return vec3{s[0], s[1], s[2]} }

func (a vec3) add(b vec3) vec3      { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a vec3) sub(b vec3) vec3      { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a vec3) scale(s float64) vec3 { return vec3{a[0] * s, a[1] * s, a[2] * s} }
func (a vec3) dot(b vec3) float64   { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func (a vec3) cross(b vec3) vec3 {
	return vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func (a vec3) length() float64 { return math.Sqrt(a.dot(a)) }
func (a vec3) normalized() vec3 {
	l := a.length()
	if l == 0 {
		return a
	}
	return a.scale(1 / l)
}

type triangle [3]vec3

func (t triangle) centroid() vec3 { return t[0].add(t[1]).add(t[2]).scale(1.0 / 3) }

// mat4 is column-major, as glTF stores it.
type mat4 [16]float64

func identity() mat4 { return mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }

func (a mat4) mul(b mat4) mat4 {
	var out mat4
	for col := range 4 {
		for row := range 4 {
			var sum float64
			for k := range 4 {
				sum += a[k*4+row] * b[col*4+k]
			}
			out[col*4+row] = sum
		}
	}
	return out
}

func (a mat4) apply(v vec3) vec3 {
	return vec3{
		a[0]*v[0] + a[4]*v[1] + a[8]*v[2] + a[12],
		a[1]*v[0] + a[5]*v[1] + a[9]*v[2] + a[13],
		a[2]*v[0] + a[6]*v[1] + a[10]*v[2] + a[14],
	}
}

// compose builds T * R * S from a glTF node's translation, rotation
// quaternion (x, y, z, w) and scale.
func compose(t vec3, q [4]float64, s vec3) mat4 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	return mat4{
		(1 - 2*(y*y+z*z)) * s[0], (2 * (x*y + z*w)) * s[0], (2 * (x*z - y*w)) * s[0], 0,
		(2 * (x*y - z*w)) * s[1], (1 - 2*(x*x+z*z)) * s[1], (2 * (y*z + x*w)) * s[1], 0,
		(2 * (x*z + y*w)) * s[2], (2 * (y*z - x*w)) * s[2], (1 - 2*(x*x+y*y)) * s[2], 0,
		t[0], t[1], t[2], 1,
	}
}

// closestPointOnTriangle is Ericson's region test (Real-Time Collision
// Detection, 5.1.5).
func closestPointOnTriangle(p vec3, t triangle) vec3 {
	a, b, c := t[0], t[1], t[2]
	ab, ac, ap := b.sub(a), c.sub(a), p.sub(a)
	d1, d2 := ab.dot(ap), ac.dot(ap)
	if d1 <= 0 && d2 <= 0 {
		return a
	}
	bp := p.sub(b)
	d3, d4 := ab.dot(bp), ac.dot(bp)
	if d3 >= 0 && d4 <= d3 {
		return b
	}
	vc := d1*d4 - d3*d2
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		return a.add(ab.scale(d1 / (d1 - d3)))
	}
	cp := p.sub(c)
	d5, d6 := ab.dot(cp), ac.dot(cp)
	if d6 >= 0 && d5 <= d6 {
		return c
	}
	vb := d5*d2 - d1*d6
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		return a.add(ac.scale(d2 / (d2 - d6)))
	}
	va := d3*d6 - d5*d4
	if va <= 0 && d4-d3 >= 0 && d5-d6 >= 0 {
		return b.add(c.sub(b).scale((d4 - d3) / ((d4 - d3) + (d5 - d6))))
	}
	denom := 1 / (va + vb + vc)
	return a.add(ab.scale(vb * denom)).add(ac.scale(vc * denom))
}
