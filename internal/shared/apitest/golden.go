// Package apitest pins the JSON shapes /api/v1 returns.
//
// The JSON a client sees is a contract, and the failure that matters is a
// field renamed or dropped in a refactor. Per-field assertions miss exactly
// that, because nobody writes an assertion for a field they deleted, so the
// whole encoded shape is pinned instead, built from fixed values with no
// database and no model involved.
//
// The golden files are also read by the iOS client's tests, which decode each
// one with the types generated from docs/api/openapi.yaml. A golden file the
// generated client cannot decode means the spec and the server disagree.
//
// Import only from tests.
package apitest

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the API contract golden files in testdata/")

// AssertGolden encodes value as the API would and compares it with
// testdata/<name>. Run the test with -update to write the file instead.
func AssertGolden(t *testing.T, name string, value any) {
	t.Helper()

	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded = append(encoded, '\n')

	path := filepath.Join("testdata", name)
	if *update {
		if mkErr := os.MkdirAll("testdata", 0o755); mkErr != nil {
			t.Fatalf("mkdir testdata: %v", mkErr)
		}
		if writeErr := os.WriteFile(path, encoded, 0o644); writeErr != nil {
			t.Fatalf("write %s: %v", path, writeErr)
		}
		t.Logf("wrote %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", path, err)
	}
	if string(want) != string(encoded) {
		t.Errorf("the API shape changed. Review the diff: it is what every client sees, and docs/api/openapi.yaml must say the same.\n\nwant:\n%s\n\ngot:\n%s", want, encoded)
	}
}
