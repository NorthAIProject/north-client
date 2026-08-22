package quota

// Limits maps a user tier to the budgets that serve it, falling back to a
// shared default for tiers with no entry of their own.
//
// The same shape as ai.ChainSet, and for the same reasons: the tier arrives as
// a plain string because this package has no business importing users to learn
// what a tier is, and a tier with no entry of its own must fall back rather
// than resolve to nothing.
//
// # Why only the count varies between tiers
//
// A budget has two halves, and only one of them is safe to change per tier.
// quota_counters is keyed on (user_id, action, window_start), and window_start
// is the window length floored against the epoch — see internal/quota/db/
// queries.sql. Give two tiers different window lengths and a user who changes
// tier mid-window lands on a different row, which silently resets their count
// and hands them a fresh budget for upgrading. Vary PerWindow; leave Window
// alone. NewLimits enforces this rather than trusting the caller.
type Limits struct {
	byTier   map[string]map[Action]Limit
	fallback map[Action]Limit
}

// NewLimits builds the mapping. Empty tiers are discarded, so an unset
// environment variable does not produce a tier that resolves to no budget at
// all — which, given that an unconfigured action is unbounded, would mean an
// unset variable quietly removing every limit for that tier.
//
// Every window is normalised to the fallback's window for that action, for the
// reason in the type doc. A tier that names a different window does not get it.
func NewLimits(fallback map[Action]Limit, byTier map[string]map[Action]Limit) Limits {
	set := Limits{fallback: normalise(fallback, nil)}

	for tier, limits := range byTier {
		if tier == "" || len(limits) == 0 {
			continue
		}
		if set.byTier == nil {
			set.byTier = make(map[string]map[Action]Limit, len(byTier))
		}
		set.byTier[tier] = normalise(limits, set.fallback)
	}

	return set
}

// For returns the budgets to apply for a tier.
func (l Limits) For(tier string) map[Action]Limit {
	if limits, ok := l.byTier[tier]; ok {
		return limits
	}
	return l.fallback
}

// normalise copies the map, defaults a missing window, and pins every window to
// the one the base map uses for that action.
func normalise(limits, base map[Action]Limit) map[Action]Limit {
	copied := make(map[Action]Limit, len(limits))

	for action, limit := range limits {
		if anchor, ok := base[action]; ok {
			limit.Window = anchor.Window
		}
		if limit.Window <= 0 {
			limit.Window = DefaultWindow
		}
		copied[action] = limit
	}

	return copied
}
