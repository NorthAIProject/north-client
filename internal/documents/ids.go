package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// ChunkIDPrefix marks a chunk id wherever one appears — in a citation, in a
// log, in the evidence_refs column of a stored reply.
const ChunkIDPrefix = "nor_chk_"

// ChunkID derives a chunk's identifier from the document, the chunk's position
// within it, and a hash of its content.
//
// Deterministic on purpose. Re-chunking unchanged text produces the identical
// id, so a reindex writes only what actually changed and a chunk id quoted in a
// reply from three months ago still resolves. A random id would make every
// reindex rewrite the whole table and silently break every stored citation.
//
// Content is part of the input, not just position: an edited passage is a
// different passage, and it should not inherit the identity — or the citations
// — of the text it replaced.
func ChunkID(documentID uuid.UUID, ordinal int, contentSHA256 string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d\x00%s", documentID, ordinal, contentSHA256))
	return ChunkIDPrefix + hex.EncodeToString(sum[:])[:32]
}
