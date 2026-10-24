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
	License string              `json:"license"`
	Coin    Coin                `json:"coin"`
	Stroke  float64             `json:"stroke"`
	Areas   map[string]Area     `json:"areas"`
	Monthly Monthly             `json:"monthly"`
	Glyphs  map[string][]string `json:"glyphs"`
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

// Monthly is the monthly challenge coin: a gold rim, a deep face and no glyph.
// The month label sits above centre and the target number below it, both
// centred. FontWeight is a numeric CSS weight. FontFamily is an intent: "rounded"
// is the system rounded face (SF Rounded on iOS, ui-rounded then system-ui on
// web). Anchor "middle" centres each line on x = ViewBox/2. Baseline true means
// MonthY and NumberY are text baselines. A target of NumberWideFrom or more is drawn
// at NumberSize * NumberWideScale so it stays inside the face.
type Monthly struct {
	Rim  string `json:"rim"`
	Fill string `json:"fill"`
	Text struct {
		Color           string  `json:"color"`
		MonthSize       float64 `json:"monthSize"`
		MonthY          float64 `json:"monthY"`
		NumberSize      float64 `json:"numberSize"`
		NumberY         float64 `json:"numberY"`
		NumberWideFrom  int     `json:"numberWideFrom"`
		NumberWideScale float64 `json:"numberWideScale"`
		FontWeight      int     `json:"fontWeight"`
		FontFamily      string  `json:"fontFamily"`
		Anchor          string  `json:"anchor"`
		Baseline        bool    `json:"baseline"`
	} `json:"text"`
}
