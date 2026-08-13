package documents_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/documents/document"
)

// The zero value is what every caller in the tree actually passes. If it
// fingerprinted differently from the defaults it resolves to, the first index
// pass after this shipped would mark every document in the product as read by
// an older reader — a full rechunk of everything, for nothing.
func TestTheZeroOptionsFingerprintAsTheDefaults(t *testing.T) {
	zero := documents.Options{}.Fingerprint()
	explicit := documents.Options{
		MaxChars:     documents.DefaultMaxChars,
		OverlapChars: documents.DefaultOverlapChars,
	}.Fingerprint()

	if zero != explicit {
		t.Errorf("zero value fingerprints %q, explicit defaults %q", zero, explicit)
	}
	if zero == "" {
		t.Error("fingerprint is empty, which is the value that means never indexed")
	}
	if again := (documents.Options{}).Fingerprint(); again != zero {
		t.Errorf("fingerprint is not stable across calls: %q then %q", zero, again)
	}
}

func TestChangingTheBoundsChangesTheFingerprint(t *testing.T) {
	base := documents.Options{MaxChars: 2400, OverlapChars: 200}

	wider := documents.Options{MaxChars: 4000, OverlapChars: 200}
	if base.Fingerprint() == wider.Fingerprint() {
		t.Error("a different MaxChars fingerprints the same")
	}

	overlapped := documents.Options{MaxChars: 2400, OverlapChars: 400}
	if base.Fingerprint() == overlapped.Fingerprint() {
		t.Error("a different OverlapChars fingerprints the same")
	}
}

// The case the column exists for: unchanged text, changed reader.
func TestIndexedWithNoticesANewReader(t *testing.T) {
	const (
		sha  = "abc123"
		mine = "reader-one"
	)

	doc := document.Document{ContentSHA256: sha, ChunkerFingerprint: mine}

	if !doc.IndexedWith(sha, mine) {
		t.Error("same text and same reader reported as needing work")
	}
	if doc.IndexedWith("different", mine) {
		t.Error("changed text reported as already indexed")
	}
	if doc.IndexedWith(sha, "reader-two") {
		t.Error("same text under different bounds reported as already indexed")
	}

	// A document indexed before the column existed carries an empty
	// fingerprint, which must never match a real one.
	older := document.Document{ContentSHA256: sha}
	if older.IndexedWith(sha, mine) {
		t.Error("a document indexed by an unknown reader reported as up to date")
	}
}

func TestReadByAnotherChunkerOnlyAppliesToReadDocuments(t *testing.T) {
	const mine = "reader-one"

	ready := document.Document{Status: document.StatusReady, ChunkerFingerprint: "reader-two"}
	if !ready.ReadByAnotherChunker(mine) {
		t.Error("a ready document with foreign chunks was not reported")
	}

	current := document.Document{Status: document.StatusReady, ChunkerFingerprint: mine}
	if current.ReadByAnotherChunker(mine) {
		t.Error("a ready document read by this reader was reported")
	}

	// A document that has never been read has no chunks to be wrong, and
	// naming it here would put it in the attention list twice.
	pending := document.Document{Status: document.StatusPending}
	if pending.ReadByAnotherChunker(mine) {
		t.Error("a document that was never read was reported as read an older way")
	}
}
