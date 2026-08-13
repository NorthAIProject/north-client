package coach

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
)

// Evidence kinds. The prefix is part of the ref because the two kinds resolve
// against different tables.
//
// Defined by internal/conversations, which owns the column they are stored in;
// see the constants there for why.
const (
	EvidenceKindMemory = conversations.EvidenceKindMemory
	EvidenceKindChunk  = conversations.EvidenceKindChunk
)

// MemoryRef builds the ref for a stored profile fact.
func MemoryRef(id uuid.UUID) string { return EvidenceKindMemory + ":" + id.String() }

// ChunkRef builds the ref for a document chunk.
func ChunkRef(chunkID string) string { return EvidenceKindChunk + ":" + chunkID }

// refPattern matches a citation as the model is asked to write it.
//
// Deliberately narrow: a kind from a known set, then an id of the characters
// those ids actually use. A looser pattern would strip anything a user happened
// to write in double brackets out of their own conversation.
var refPattern = regexp.MustCompile(`\[\[(memory|chunk):([A-Za-z0-9_-]{1,80})\]\]`)

// refRemoval is refPattern plus the space that usually precedes a citation.
//
// Removing the citation alone leaves "fasted  most days" with a doubled space.
// The saved text gets that repaired by tidy, but the streamed text cannot —
// it has already gone out — so the two would disagree about their own spacing
// until the page was reloaded. Taking the space with the citation keeps what
// the reader watches identical to what is stored.
var refRemoval = regexp.MustCompile(` ?\[\[(memory|chunk):([A-Za-z0-9_-]{1,80})\]\]`)

// StripRefs removes citations from a reply and returns the ones that were real.
//
// Two jobs, together because they read the same text once. The visible reply
// should not contain machine handles, and the refs worth recording are only
// those that were actually in the context block: a model asked to cite will
// sometimes produce a well-formed ref for a fact it was never given, and
// storing that would make the audit trail lie in the one direction that
// matters.
//
// known is the set of refs offered to the model for this turn. Refs outside it
// are stripped from the text like any other, but not returned.
func StripRefs(reply string, known map[string]bool) (string, []string) {
	if !strings.Contains(reply, "[[") {
		return reply, nil
	}

	var (
		used = make([]string, 0, 4)
		seen = make(map[string]bool, 4)
	)

	cleaned := refRemoval.ReplaceAllStringFunc(reply, func(match string) string {
		parts := refRemoval.FindStringSubmatch(match)
		ref := parts[1] + ":" + parts[2]
		if known[ref] && !seen[ref] {
			seen[ref] = true
			used = append(used, ref)
		}
		return ""
	})

	return tidy(cleaned), used
}

// OfferedRefs is the set of refs the context block put in front of the model.
func (c *Context) OfferedRefs() map[string]bool {
	out := make(map[string]bool, len(c.Memories)+len(c.KnowledgeHits))
	for _, e := range c.Memories {
		out[e.Ref] = true
	}
	for _, e := range c.KnowledgeHits {
		out[e.Ref] = true
	}
	return out
}

// tidy repairs the spacing a removed citation leaves behind.
//
// Removing "[[memory:...]]" from "she trains fasted [[memory:x]], so ..." would
// otherwise leave a space before the comma. Not cosmetic: the reply is the
// product, and stray spacing reads as a bug in the model rather than in us.
func tidy(s string) string {
	s = strings.ReplaceAll(s, " \n", "\n")
	for _, punct := range []string{".", ",", ";", ":", "!", "?", ")"} {
		s = strings.ReplaceAll(s, " "+punct, punct)
	}
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// maxRefLen bounds how much text the stream filter will hold back waiting to
// see whether an open bracket becomes a citation. Longer than any real ref.
const maxRefLen = 100

// refStripper removes citations from a reply as it streams.
//
// Needed because the refs reach the browser before anything has seen the whole
// reply: the model writes "[[memory:...]]" mid-sentence and the user watches it
// appear. Stripping only at save time would show every reader the machinery.
//
// A citation can be split across chunk boundaries — "[[mem" in one, "ory:..."
// in the next — so the filter holds back the tail from the last unmatched "["
// until it can tell whether it is a citation or an ordinary bracket. The hold
// is released after maxRefLen characters, so prose that merely contains "["
// stalls the stream by at most that much.
type refStripper struct {
	held strings.Builder
}

// Take returns the part of s that is safe to forward.
func (f *refStripper) Take(s string) string {
	f.held.WriteString(s)
	buffered := refRemoval.ReplaceAllString(f.held.String(), "")
	f.held.Reset()

	open := holdFrom(buffered)
	if open < 0 || len(buffered)-open > maxRefLen {
		return buffered
	}

	f.held.WriteString(buffered[open:])
	return buffered[:open]
}

// holdFrom returns where to stop forwarding, or -1 to forward everything.
//
// Two things are held. An unclosed bracket run, because it may still become a
// citation. And a trailing space, because the space *before* a citation is
// removed with it — and once that space has been forwarded it cannot be taken
// back, which is how the streamed reply ends up with a doubled space that the
// saved one does not have.
//
// Holding a trailing space costs one space of latency: it goes out as soon as
// the next chunk arrives, or at Flush if the reply ends there.
func holdFrom(s string) int {
	i := openBracketRun(s)
	if i < 0 {
		if strings.HasSuffix(s, " ") {
			return len(s) - 1
		}
		return -1
	}
	if i > 0 && s[i-1] == ' ' {
		i--
	}
	return i
}

// Flush returns whatever was still held when the stream ended.
func (f *refStripper) Flush() string {
	out := refRemoval.ReplaceAllString(f.held.String(), "")
	f.held.Reset()
	return out
}

// openBracketRun returns where the trailing, still-unclosed bracket run starts,
// or -1 when there is none.
//
// It walks back over consecutive '[' rather than stopping at the last one:
// after "[[" arrives, the last '[' is the second of the pair, and holding from
// there would forward the first — leaving a citation that can never be matched
// once its opener has already reached the reader.
func openBracketRun(s string) int {
	i := strings.LastIndex(s, "[")
	if i < 0 {
		return -1
	}
	for i > 0 && s[i-1] == '[' {
		i--
	}
	return i
}

// ParseRef splits a ref into its kind and id.
func ParseRef(ref string) (kind, id string, err error) {
	kind, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return "", "", fmt.Errorf("malformed evidence ref %q", ref)
	}
	if kind != EvidenceKindMemory && kind != EvidenceKindChunk {
		return "", "", fmt.Errorf("unknown evidence kind %q", kind)
	}
	return kind, id, nil
}
