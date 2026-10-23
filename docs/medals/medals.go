// Package medals embeds medals.json, the one source for the colours and glyph
// paths of every coin medal. Web renders from it here; iOS copies the file into
// NorthKit. The glyph paths are lucide icons; their licence (ISC, with Feather's
// MIT portion) is LICENSE-lucide.txt next to this file.
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
	License string          `json:"license"`
	Coin    Coin            `json:"coin"`
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

// Coin is the geometry of a coin in a ViewBox x ViewBox square, shared by web
// and iOS so neither copies magic numbers.
type Coin struct {
	ViewBox    float64 `json:"viewBox"`
	RimRadius  float64 `json:"rimRadius"`
	FaceRadius float64 `json:"faceRadius"`
	FaceEdge   struct {
		Color string  `json:"color"`
		Width float64 `json:"width"`
	} `json:"faceEdge"`
	Glyph struct {
		Translate float64 `json:"translate"`
		Scale     float64 `json:"scale"`
	} `json:"glyph"`
	Scallop struct {
		Bumps      int     `json:"bumps"`
		BaseRadius float64 `json:"baseRadius"`
		Depth      float64 `json:"depth"`
		Points     int     `json:"points"`
	} `json:"scallop"`
	Locked struct {
		Opacity   float64 `json:"opacity"`
		Grayscale float64 `json:"grayscale"`
	} `json:"locked"`
}
