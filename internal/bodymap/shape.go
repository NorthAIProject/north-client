package bodymap

// Shape is how far the figure's skin is pushed out from the mesh as modelled,
// per morph target, each 0 (as modelled) to 1 (the target at full strength).
// The server works it out from a person's measurements so every client draws
// the same body from the same four numbers.
type Shape struct {
	Waist   float64 `json:"waist"`
	Hips    float64 `json:"hips"`
	Chest   float64 `json:"chest"`
	Overall float64 `json:"overall"`
}

// Zero reports a shape that leaves the figure as modelled.
func (s Shape) Zero() bool {
	return s.Waist == 0 && s.Hips == 0 && s.Chest == 0 && s.Overall == 0
}

// Girth is one morph target's reach, as circumference over standing height:
// From is the mesh as modelled, To the same band with the target at 1.
// Height-relative, so one figure serves a tall and a short person alike.
type Girth struct {
	From, To float64
}

// Baseline is the mesh's own girths at each band. Measured off the shell by
// scripts/bodymap when it writes the morph targets; until those exist these are
// typical adult ratios, so a shape can be computed and stored ahead of the mesh.
type Baseline struct {
	Waist, Hips, Chest Girth
}

// ShellBaseline is the figure's baseline. Replaced by generated values when
// the shell gains morph targets.
var ShellBaseline = Baseline{
	Waist: Girth{From: 0.43, To: 0.72},
	Hips:  Girth{From: 0.52, To: 0.76},
	Chest: Girth{From: 0.53, To: 0.74},
}
