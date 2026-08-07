package ai

// ChainSet maps a user tier to the ordered list of providers that should serve
// it, falling back to a shared default for tiers with no entry of their own.
//
// The tier arrives as a plain string. The ai layer has no business importing
// the users package to learn what a tier is, and a chain is only ever looked up
// by name.
type ChainSet struct {
	byTier   map[string][]string
	fallback []string
}

// NewChainSet builds the mapping. Empty chains in byTier are discarded, so an
// unset environment variable does not produce a tier that resolves to nothing.
func NewChainSet(fallback []string, byTier map[string][]string) ChainSet {
	set := ChainSet{fallback: fallback}

	for tier, chain := range byTier {
		if tier == "" || len(chain) == 0 {
			continue
		}
		if set.byTier == nil {
			set.byTier = make(map[string][]string, len(byTier))
		}
		set.byTier[tier] = chain
	}

	return set
}

// For returns the chain to try for a tier.
func (c ChainSet) For(tier string) []string {
	if chain, ok := c.byTier[tier]; ok {
		return chain
	}
	return c.fallback
}
