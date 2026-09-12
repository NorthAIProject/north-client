package score

// Verdict is the word shown beside the number.
//
// Four bands, not ten. A word people can act on is worth more than a precise
// adjective nobody can distinguish from the one above it.
type Verdict string

const (
	// VerdictUnknown is a domain with too little logged to judge. It is not a
	// bad score — it is the absence of one, and it must never render as "Low".
	VerdictUnknown Verdict = "unknown"

	VerdictLow    Verdict = "low"
	VerdictUneven Verdict = "uneven"
	VerdictOK     Verdict = "ok"
	VerdictStrong Verdict = "strong"
)

// Verdict bands the score.
func (s Score) Verdict() Verdict {
	if !s.HasData {
		return VerdictUnknown
	}
	switch {
	case s.Points >= 85:
		return VerdictStrong
	case s.Points >= 70:
		return VerdictOK
	case s.Points >= 50:
		return VerdictUneven
	default:
		return VerdictLow
	}
}

// Key names the verdict in the message catalogue.
func (v Verdict) Key() string { return "score.verdict." + string(v) }
