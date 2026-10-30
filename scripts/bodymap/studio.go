package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// The web viewer lights the figure with a studio of five glowing panels
// (createStudioEnvironment in north-web-app's muscle viewer, which is the
// source of truth; keep this table the same). iOS cannot run
// that code, so the same rig is rendered here into an equirectangular image
// for RealityKit's ImageBasedLightComponent. One light rig, two renderers.

type studioPanel struct {
	hex           uint32
	intensity     float64
	width, height float64
	position      vec3
}

var studioPanels = []studioPanel{
	{0xfff4e6, 6.5, 9, 12, vec3{5, 5, 7}},   // key, warm, front-right
	{0xdce8ff, 2.4, 8, 10, vec3{-6, 2, 3}},  // fill, cool, front-left
	{0xffffff, 3.5, 10, 4, vec3{0, 9, -1}},  // overhead strip
	{0x6b6257, 0.8, 12, 12, vec3{0, -6, 2}}, // floor bounce
	{0xa8c4e8, 1.4, 7, 10, vec3{0, 1, -9}},  // back separation
}

// studioPeak is the brightest panel's intensity: the image stores radiance
// divided by it, and the app scales back up with intensityExponent.
const studioPeak = 6.5

// studioRadiance is the light arriving from direction dir: the colour of the
// nearest panel the ray from the origin hits, or black.
func studioRadiance(dir vec3) vec3 {
	best, out := math.Inf(1), vec3{}
	for _, p := range studioPanels {
		// three.js lookAt(0, 0, 0): the panel faces the origin, with its
		// x axis level and its y axis as upright as that allows.
		z := p.position.normalized()
		if math.Abs(z[1]) > 0.9999 {
			z[2] += 1e-4
			z = z.normalized()
		}
		x := vec3{0, 1, 0}.cross(z).normalized()
		y := z.cross(x)
		denom := dir.dot(z)
		if denom <= 0 {
			continue
		}
		t := p.position.dot(z) / denom
		if t <= 0 || t >= best {
			continue
		}
		hit := dir.scale(t).sub(p.position)
		if math.Abs(hit.dot(x)) > p.width/2 || math.Abs(hit.dot(y)) > p.height/2 {
			continue
		}
		best = t
		out = vec3{float64(p.hex>>16&0xff) / 255, float64(p.hex>>8&0xff) / 255, float64(p.hex&0xff) / 255}.scale(p.intensity)
	}
	return out
}

// equirectDirection is the world direction a pixel of a w×h equirectangular
// image looks along: the image's centre is +z, its left edge -x. If an app
// maps the centre to -z instead, turn the image half a turn in the app.
func equirectDirection(x, y, w, h int) vec3 {
	lon := ((float64(x)+0.5)/float64(w) - 0.5) * 2 * math.Pi
	lat := (0.5 - (float64(y)+0.5)/float64(h)) * math.Pi
	return vec3{math.Sin(lon) * math.Cos(lat), math.Sin(lat), math.Cos(lon) * math.Cos(lat)}
}

// writeStudioPNG renders the rig as an sRGB PNG, radiance scaled by
// studioPeak so the key light is white.
func writeStudioPNG(path string, w, h int) error {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := studioRadiance(equirectDirection(x, y, w, h))
			img.Set(x, y, color.NRGBA{srgb8(c[0] / studioPeak), srgb8(c[1] / studioPeak), srgb8(c[2] / studioPeak), 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func srgb8(linear float64) uint8 {
	v := min(max(linear, 0), 1)
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return uint8(math.Round(v * 255))
}
