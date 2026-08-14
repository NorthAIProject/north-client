package coach

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
)

// Evidence kinds. The prefix is part of the ref because the two kinds resolve
// against different tables.
//
// Defined by internal/conversations, which owns the column they are stored in;
// see the constants there for why.
const (
	EvidenceKindMemory   = conversations.EvidenceKindMemory
	EvidenceKindChunk    = conversations.EvidenceKindChunk
	EvidenceKindExercise = conversations.EvidenceKindExercise
)

// MemoryRef builds the ref for a stored profile fact.
func MemoryRef(id uuid.UUID) string { return EvidenceKindMemory + ":" + id.String() }

// ChunkRef builds the ref for a document chunk.
func ChunkRef(chunkID string) string { return EvidenceKindChunk + ":" + chunkID }

// ToolGetExercise is the capability that reads one catalogue exercise, and
// ExerciseArgs is what it takes.
//
// Declared here rather than in internal/agent, where the capability is built,
// because this package cannot import that one: agent reaches calculator, which
// reaches back here. Since exactly one of the two directions is available, the
// name lives with the side that has to recognise it in a recorded call, and
// agent refers to these when declaring the tool.
const ToolGetExercise = "get_exercise"

// ExerciseArgs is the argument object for ToolGetExercise.
type ExerciseArgs struct {
	Slug string `json:"slug"`
}

// ExerciseRef builds the ref for a catalogue exercise the coach looked up.
//
// Deliberately absent from refRemoval's pattern: the other two kinds are
// citations the model writes into its prose and this one is not. Nothing should
// be stripped out of a reply on account of it.
func ExerciseRef(slug string) string { return EvidenceKindExercise + ":" + slug }

// appendExerciseLookups records the exercises a round of tool calls read.
//
// Reads the call rather than the result, because the result is prose written
// for the model and the slug is the thing the catalogue is addressed by. A call
// whose arguments will not decode is skipped: the tool itself is about to
// reject it, and a malformed argument should cost a picture rather than a
// reply.
func appendExerciseLookups(into []string, calls []ai.ToolCall) []string {
	for _, call := range calls {
		if call.Name != ToolGetExercise {
			continue
		}

		var args ExerciseArgs
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			continue
		}

		slug := strings.TrimSpace(args.Slug)
		if slug == "" || slices.Contains(into, slug) {
			continue
		}
		into = append(into, slug)
	}
	return into
}

// refRemoval matches a citation plus the usual leading space.
//
// Deliberately narrow: a kind from a known set, then an id of the characters
// those ids actually use. A looser pattern would strip anything a user happened
// to write in double brackets out of their own conversation.
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
