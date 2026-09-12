package score

// Reason names the component that carried a domain and the one that cost it.
//
// Keys, not prose. The sentence is assembled by the view layer through the
// message catalogue, which is the only shape that survives four locales and
// the only one a Telegram digest can reuse unchanged.
type Reason struct {
	Key   string
	Best  string
	Worst string
}

// reasonKey is the catalogue entry the two component keys are rendered into.
const reasonKey = "score.reason"

// Reason contrasts the strongest and weakest measured component.
//
// It reports false rather than inventing a story: a domain with one known
// component has nothing to contrast, and one where everything scored alike has
// no standout to name.
func (s Score) Reason() (Reason, bool) {
	if !s.HasData {
		return Reason{}, false
	}

	var best, worst Component
	var found bool
	for _, c := range s.Components {
		if !c.Known || c.Weight <= 0 {
			continue
		}
		if !found {
			best, worst, found = c, c, true
			continue
		}
		if ratioLess(best, c) {
			best = c
		}
		if ratioLess(c, worst) {
			worst = c
		}
	}

	if !found || best.Key == worst.Key {
		return Reason{}, false
	}
	return Reason{Key: reasonKey, Best: best.Key, Worst: worst.Key}, true
}

// ratioLess reports whether a scored a smaller share of its weight than b.
// Cross-multiplied rather than divided so two components with different
// weights compare exactly, with no float rounding deciding a tie.
func ratioLess(a, b Component) bool {
	return a.Earned*b.Weight < b.Earned*a.Weight
}
