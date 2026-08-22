// Package pricing turns token counts into money.
//
// Rates live in pricing.json, embedded and versioned, for the same reason
// prompts live in .md files: a price is product surface, and a change to one
// should be a reviewable diff with a history rather than a literal buried in a
// service. No other package may contain a rate.
//
// # Why an unknown model is not an error
//
// Cost reports whether it knew the rate. A caller that does not know what a
// call cost still has to let the call succeed — refusing to answer a user
// because a price is missing from a table would be an outage caused by
// bookkeeping. The gap is recorded instead: the ledger stores a cost of zero,
// and spend.Repository.CountUnpriced surfaces it at the top of the report so an
// understated total is visible as one.
//
// The test in this package is what stops that from being a silent state for
// long: every model the shipped configuration can reach must be either priced
// or listed in "unpriced", so adding a provider without deciding what it costs
// fails the build.
package pricing

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed pricing.json
var files embed.FS

// Rate is what one model charges, in micros per million tokens.
//
// Per million rather than per token because that is how every vendor quotes it,
// and because per-token integers would round to zero.
type Rate struct {
	Input  int64  `json:"input"`
	Output int64  `json:"output"`
	Note   string `json:"note,omitempty"`
}

type table struct {
	Models map[string]Rate `json:"models"`

	// Unpriced is the acknowledgement list: models known to exist with no rate
	// filled in yet. Present so the coverage test can tell "nobody has done
	// this" from "this was forgotten".
	Unpriced []string `json:"unpriced"`
}

var (
	once    sync.Once
	loaded  table
	loadErr error
)

func load() (table, error) {
	once.Do(func() {
		body, err := files.ReadFile("pricing.json")
		if err != nil {
			loadErr = fmt.Errorf("pricing: read table: %w", err)
			return
		}
		if err := json.Unmarshal(body, &loaded); err != nil {
			loadErr = fmt.Errorf("pricing: parse table: %w", err)
			return
		}
	})
	return loaded, loadErr
}

// Key is how a provider and model are named in the table.
//
// The provider is normalised first: a chain entry may be written
// "openrouter=z-ai/glm-5.2:free", and the client built from it reports that
// whole string as its name. Only the part before the first "=" identifies the
// backend, and the split is on the first "=" alone because a model slug carries
// its own punctuation.
func Key(provider, model string) string {
	base, _, _ := strings.Cut(provider, "=")
	return base + "/" + model
}

// Cost prices a call, returning micros and whether a rate was known.
//
// Integer arithmetic throughout. The multiplication happens before the division
// so a call smaller than a million tokens does not floor to nothing, which is
// most calls.
func Cost(provider, model string, inputTokens, outputTokens int) (int64, bool) {
	t, err := load()
	if err != nil {
		return 0, false
	}

	rate, ok := t.Models[Key(provider, model)]
	if !ok {
		return 0, false
	}

	const perMillion = 1_000_000
	micros := (int64(inputTokens)*rate.Input)/perMillion +
		(int64(outputTokens)*rate.Output)/perMillion

	return micros, true
}

// Known reports whether a rate exists for the model.
func Known(provider, model string) bool {
	t, err := load()
	if err != nil {
		return false
	}
	_, ok := t.Models[Key(provider, model)]
	return ok
}

// Acknowledged reports whether a model is on the unpriced list — known to
// exist, deliberately without a rate. Used by the coverage test to tell an
// accepted gap from an oversight.
func Acknowledged(key string) bool {
	t, err := load()
	if err != nil {
		return false
	}
	for _, k := range t.Unpriced {
		if k == key {
			return true
		}
	}
	return false
}

// Models returns every priced key, for a test or a report.
func Models() []string {
	t, err := load()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(t.Models))
	for k := range t.Models {
		out = append(out, k)
	}
	return out
}
