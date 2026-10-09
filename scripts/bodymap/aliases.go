package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// The mesh-name → muscle-key table is MUSCLE_ALIASES in
// web/assets/js/shared/muscle-viewer/muscles.js, which tools/model/build-body.mjs
// also reads to decide which atlas meshes go into body.glb. Reading it here
// instead of copying it keeps one table. The file's shape is fixed and
// hand-maintained; internal/workouts/plan/muscle_test.go parses it the same way.

var (
	aliasObject  = regexp.MustCompile(`(?ms)^export const MUSCLE_ALIASES = \{$(.*?)^\};$`)
	aliasEntry   = regexp.MustCompile(`(?s)\n  ([a-z]+): \[(.*?)\],?`)
	quotedString = regexp.MustCompile(`"([^"]+)"`)
)

type aliasTable struct {
	keys    []string          // in file order
	lookup  map[string]string // normalized mesh name → key
	ordered [][2]string       // (normalized alias, key) in file order, for the containment fallback
}

func readAliases(path string) (aliasTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return aliasTable{}, err
	}
	body := aliasObject.FindSubmatch(data)
	if body == nil {
		return aliasTable{}, fmt.Errorf("%s: no MUSCLE_ALIASES object", path)
	}
	table := aliasTable{lookup: map[string]string{}}
	for _, entry := range aliasEntry.FindAllSubmatch(body[1], -1) {
		key := string(entry[1])
		table.keys = append(table.keys, key)
		for _, alias := range quotedString.FindAllSubmatch(entry[2], -1) {
			normalized := normalizeName(string(alias[1]))
			table.lookup[normalized] = key
			table.ordered = append(table.ordered, [2]string{normalized, key})
		}
	}
	if len(table.keys) == 0 {
		return aliasTable{}, fmt.Errorf("%s: parsed no keys out of MUSCLE_ALIASES", path)
	}
	return table, nil
}

var (
	separators     = regexp.MustCompile(`[_.]+`)
	trailingDigits = regexp.MustCompile(`\d+$`)
	spaces         = regexp.MustCompile(`\s+`)
)

// normalizeName mirrors normalizeName() in muscles.js.
func normalizeName(name string) string {
	s := strings.ToLower(name)
	s = separators.ReplaceAllString(s, " ")
	s = trailingDigits.ReplaceAllString(s, "")
	s = spaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// resolve mirrors resolveKey() in muscles.js: exact match, then containment.
func (a aliasTable) resolve(name string) (string, bool) {
	n := normalizeName(name)
	if n == "" {
		return "", false
	}
	if key, ok := a.lookup[n]; ok {
		return key, true
	}
	for _, pair := range a.ordered {
		if strings.Contains(n, pair[0]) || strings.Contains(pair[0], n) {
			return pair[1], true
		}
	}
	return "", false
}
