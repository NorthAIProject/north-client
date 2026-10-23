// Package medals embeds medals.json, the one source for the colours and glyph
// paths of every coin medal. Web renders from it here; iOS copies the file into
// NorthKit.
package medals

import (
	_ "embed"
	"encoding/json"
)

//go:embed medals.json
var raw []byte

// Raw is the file as shipped, for tooling that copies it.
func Raw() []byte { return raw }

// Area is the face and rim colour of one area.
type Area struct {
	Fill string `json:"fill"`
	Rim  string `json:"rim"`
}

// Set is the decoded file. Glyphs are lucide 24x24 paths, stroked at Stroke.
type Set struct {
	Stroke  float64         `json:"stroke"`
	Areas   map[string]Area `json:"areas"`
	Monthly struct {
		Rim string `json:"rim"`
	} `json:"monthly"`
	Glyphs map[string][]string `json:"glyphs"`
}

// Load decodes the embedded file.
func Load() Set {
	var s Set
	if err := json.Unmarshal(raw, &s); err != nil {
		panic("medals.json: " + err.Error())
	}
	return s
}
