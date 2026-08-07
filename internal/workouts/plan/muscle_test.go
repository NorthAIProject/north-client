package plan

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// musclesJS is the client-side half of the muscle vocabulary. muscle.go's doc
// comment describes a two-file sync checklist; these tests are what stops the
// checklist from being the only thing enforcing it.
//
// The tables used to live in viewer.js and moved here when the asset build
// started reading them too — tools/model/build-body.mjs decides which meshes go
// into body.glb from this same list, so the model and the code that reads it
// cannot disagree.
const musclesJS = "../../../web/assets/js/shared/muscle-viewer/muscles.js"

func TestEveryMuscleGroupIsKnownToTheViewer(t *testing.T) {
	t.Parallel()

	source := readMuscleTables(t)
	aliases := keysOfJSObject(t, source, "MUSCLE_ALIASES")
	info := keysOfJSObject(t, source, "MUSCLE_INFO")

	for _, key := range MuscleGroups {
		if !aliases[key] {
			t.Errorf("%q is in MuscleGroups but has no MUSCLE_ALIASES entry — the viewer can never colour it", key)
		}
		if !info[key] {
			t.Errorf("%q is in MuscleGroups but has no MUSCLE_INFO entry — clicking it shows nothing", key)
		}
	}

	// The reverse direction matters just as much: a key the viewer knows but
	// the schema cannot emit is dead weight that reads as a live feature.
	for key := range aliases {
		if !IsMuscleGroup(key) {
			t.Errorf("MUSCLE_ALIASES has %q, which is not in MuscleGroups — the AI schema can never produce it", key)
		}
	}
}

func TestUnmodelledGroupsAreCanonicalAndHaveNoMeshes(t *testing.T) {
	t.Parallel()

	source := readMuscleTables(t)

	for _, key := range UnmodelledGroups {
		if !IsMuscleGroup(key) {
			t.Errorf("%q is listed as unmodelled but is not a MuscleGroups key", key)
		}
		// An unmodelled key with meshes is a stale entry: it means the model
		// gained the mesh and nobody removed the apology the UI still shows.
		if got := aliasCount(t, source, key); got != 0 {
			t.Errorf("%q is listed as unmodelled but MUSCLE_ALIASES gives it %d mesh name(s) — drop it from UnmodelledGroups", key, got)
		}
	}
}

func TestNoMeshNameIsClaimedByTwoMuscleGroups(t *testing.T) {
	t.Parallel()

	// muscles.js builds ALIAS_LOOKUP as a Map in key order, so a mesh listed
	// under two groups silently belongs to whichever is written last. That is
	// a highlight quietly going to the wrong muscle, which no amount of
	// looking at the model would reveal as a bug.
	source := readMuscleTables(t)
	owner := map[string]string{}

	for _, key := range MuscleGroups {
		for _, alias := range aliasesFor(t, source, key) {
			if previous, taken := owner[alias]; taken {
				t.Errorf("mesh %q is claimed by both %q and %q; only %q would win", alias, previous, key, key)
				continue
			}
			owner[alias] = key
		}
	}
}

func readMuscleTables(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(musclesJS))
	if err != nil {
		t.Fatalf("reading the muscle tables: %v", err)
	}
	return string(data)
}

// jsObject returns the body of a top-level `const <name> = { ... };` literal.
// A regex rather than a JS parser: these two objects are hand-maintained in a
// fixed, flat shape, and a parser dependency to read them would cost more than
// the checking is worth.
func jsObject(t *testing.T, source, name string) string {
	t.Helper()

	start := regexp.MustCompile(`(?m)^(?:export )?const ` + name + ` = \{$`).FindStringIndex(source)
	if start == nil {
		t.Fatalf("could not find `const %s = {` in %s", name, musclesJS)
	}
	rest := source[start[1]:]
	end := regexp.MustCompile(`(?m)^\};$`).FindStringIndex(rest)
	if end == nil {
		t.Fatalf("could not find the end of %s in %s", name, musclesJS)
	}
	return rest[:end[0]]
}

var jsKeyPattern = regexp.MustCompile(`(?m)^  ([a-z]+): `)

func keysOfJSObject(t *testing.T, source, name string) map[string]bool {
	t.Helper()

	keys := map[string]bool{}
	for _, match := range jsKeyPattern.FindAllStringSubmatch(jsObject(t, source, name), -1) {
		keys[match[1]] = true
	}
	if len(keys) == 0 {
		t.Fatalf("parsed no keys out of %s — the parser and the file have diverged", name)
	}
	return keys
}

var jsStringPattern = regexp.MustCompile(`"([^"]+)"`)

// aliasesFor returns the mesh names listed under one MUSCLE_ALIASES key.
func aliasesFor(t *testing.T, source, key string) []string {
	t.Helper()

	body := jsObject(t, source, "MUSCLE_ALIASES")
	entry := regexp.MustCompile(`(?s)\n  ` + key + `: \[(.*?)\],?\n`).FindStringSubmatch(body)
	if entry == nil {
		return nil
	}

	var names []string
	for _, match := range jsStringPattern.FindAllStringSubmatch(entry[1], -1) {
		names = append(names, match[1])
	}
	return names
}

func aliasCount(t *testing.T, source, key string) int {
	t.Helper()
	return len(aliasesFor(t, source, key))
}
