package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// writeUSDZ writes the regions as a USDZ for RealityKit, which cannot read
// glTF. The tool writes plain USDA text and leaves the binary conversion and
// packaging to the USD tools macOS ships in /usr/bin (usdcat, usdzip), so no
// USD library is needed here. One Mesh prim per region, named by key, each
// bound to its own UsdPreviewSurface material that the app recolours.
func writeUSDZ(path string, regions []region) error {
	dir, err := os.MkdirTemp("", "bodymap-usd-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	usda := filepath.Join(dir, "BodyMap.usda")
	if writeErr := os.WriteFile(usda, []byte(usdaSource(regions)), 0o644); writeErr != nil {
		return writeErr
	}
	usdc := filepath.Join(dir, "BodyMap.usdc")
	if out, catErr := exec.Command("usdcat", usda, "-o", usdc).CombinedOutput(); catErr != nil {
		return fmt.Errorf("usdcat: %w: %s", catErr, out)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	_ = os.Remove(abs)
	cmd := exec.Command("usdzip", abs, "BodyMap.usdc")
	cmd.Dir = dir // usdzip stores paths as given; keep the archive flat
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("usdzip: %w: %s", err, out)
	}
	return nil
}

func usdaSource(regions []region) string {
	var b strings.Builder
	b.WriteString("#usda 1.0\n(\n    defaultPrim = \"BodyMap\"\n    metersPerUnit = 1\n    upAxis = \"Y\"\n)\n\n")
	b.WriteString("def Xform \"BodyMap\"\n{\n")
	for _, r := range regions {
		fmt.Fprintf(&b, "    def Mesh %q (\n        prepend apiSchemas = [\"MaterialBindingAPI\"]\n    )\n    {\n", r.key)
		b.WriteString("        uniform bool doubleSided = 0\n")
		b.WriteString("        int[] faceVertexCounts = [")
		for i := range r.tris {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("3")
		}
		b.WriteString("]\n        int[] faceVertexIndices = [")
		for i, t := range r.tris {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%d, %d, %d", t[0], t[1], t[2])
		}
		b.WriteString("]\n        rel material:binding = </BodyMap/Materials/" + r.key + ">\n")
		b.WriteString("        normal3f[] normals = " + usdVectors(r.normals) + " (\n            interpolation = \"vertex\"\n        )\n")
		b.WriteString("        point3f[] points = " + usdVectors(r.verts) + "\n")
		b.WriteString("        uniform token subdivisionScheme = \"none\"\n    }\n\n")
	}
	b.WriteString("    def Scope \"Materials\"\n    {\n")
	for _, r := range regions {
		fmt.Fprintf(&b, "        def Material %q\n        {\n", r.key)
		fmt.Fprintf(&b, "            token outputs:surface.connect = </BodyMap/Materials/%s/Surface.outputs:surface>\n", r.key)
		b.WriteString("            def Shader \"Surface\"\n            {\n")
		b.WriteString("                uniform token info:id = \"UsdPreviewSurface\"\n")
		fmt.Fprintf(&b, "                color3f inputs:diffuseColor = (%g, %g, %g)\n", idleColour[0], idleColour[1], idleColour[2])
		b.WriteString("                float inputs:metallic = 0\n                float inputs:roughness = 0.65\n")
		b.WriteString("                token outputs:surface\n            }\n        }\n")
	}
	b.WriteString("    }\n}\n")
	return b.String()
}

func usdVectors(vs []vec3) string {
	var b strings.Builder
	b.WriteString("[")
	for i, v := range vs {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "(%.5f, %.5f, %.5f)", v[0], v[1], v[2])
	}
	b.WriteString("]")
	return b.String()
}
