package main

import "testing"

// Looking at the key panel sees the key light; looking away from every panel
// sees black; the image's centre looks along +z.
func TestStudioRadiance(t *testing.T) {
	key := studioRadiance(vec3{5, 5, 7}.normalized())
	if key[0] != 6.5 || key[1] < 6 || key[2] < 5 {
		t.Errorf("key direction = %v, want the warm key at 6.5", key)
	}
	if dark := studioRadiance(vec3{1, -1, 0}.normalized()); dark != (vec3{}) {
		t.Errorf("direction between panels = %v, want black", dark)
	}
	if d := equirectDirection(256, 128, 512, 256); d[2] < 0.99 {
		t.Errorf("centre pixel looks along %v, want +z", d)
	}
	if srgb8(1) != 255 || srgb8(0) != 0 || srgb8(0.5) != 188 {
		t.Errorf("srgb8: %d %d %d", srgb8(1), srgb8(0), srgb8(0.5))
	}
}
