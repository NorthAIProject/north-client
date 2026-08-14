package documents_test

import (
	"os"
	"path/filepath"
	"testing"
)

// readPDF loads a committed fixture. Missing fixtures fail the test so CI
// cannot pass while PDF extraction is untested.
func readPDF(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s is not present: %v", name, err)
	}
	return body
}
