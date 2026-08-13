package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/documents/parse"
)

// Chunking bounds.
//
// 2400 characters is roughly a screen of prose: large enough that a retrieved
// passage carries its own argument, small enough that a hit does not spend the
// context window on the parts of the section that were not relevant. The
// overlap keeps a claim that straddles a boundary retrievable from either side.
const (
	DefaultMaxChars     = 2400
	DefaultOverlapChars = 200
)

// Chunk is one citable passage of a document.
//
// The invariant that matters, asserted in the tests: Content is exactly
// strings.Join(doc.Lines[StartLine-1:EndLine], "\n"). A chunk whose line range
// does not quote its own content produces a citation that points at the wrong
// place, which is worse than no citation — it looks like evidence.
type Chunk struct {
	Ordinal     int
	HeadingPath []string
	StartLine   int
	EndLine     int
	Content     string
	SHA256      string
}

// Options bound the chunker. The zero value uses the defaults above.
type Options struct {
	MaxChars     int
	OverlapChars int
}

// withDefaults fills in whichever bound the caller left unset.
//
// Zero means unset for both fields, which is what the type's doc comment
// promises and what every caller in the tree relies on by passing Options{}.
// Overlap treated zero as a deliberate choice until the fingerprint test
// noticed the two disagreed, which meant DefaultOverlapChars had never once
// been applied — every document in the product was chunked with no overlap at
// all, and a sentence spanning a boundary was retrievable from neither side.
//
// The cost of the reading here is that no caller can ask for genuinely zero
// overlap. None wants to: overlap exists because a passage does not stop
// mattering at the character the chunker happened to cut it at.
func (o Options) withDefaults() Options {
	if o.MaxChars <= 0 {
		o.MaxChars = DefaultMaxChars
	}
	if o.OverlapChars <= 0 || o.OverlapChars >= o.MaxChars {
		o.OverlapChars = DefaultOverlapChars
	}
	return o
}

// Fingerprint identifies the bounds a document's chunks were produced under.
//
// Stored alongside the content hash so that changing the bounds is visible.
// Without it, raising MaxChars leaves every already-indexed document holding
// chunks from the old reader while still reporting that it was read — the
// index quietly disagrees with the code that built it, and nothing says so.
//
// Defaults are resolved first, so the zero value and an explicit
// Options{DefaultMaxChars, DefaultOverlapChars} fingerprint identically. They
// chunk identically; they should not look like different readers.
func (o Options) Fingerprint() string {
	o = o.withDefaults()
	sum := sha256.Sum256(fmt.Appendf(nil, "max=%d;overlap=%d", o.MaxChars, o.OverlapChars))
	return hex.EncodeToString(sum[:])[:16]
}

// ChunkDocument splits a parsed document into heading-aware passages.
//
// Deterministic: the same document always produces the same chunks with the
// same ids, which is what lets a reindex skip unchanged content and what keeps
// a chunk id quoted in an old reply resolvable.
func ChunkDocument(doc parse.Document, opts Options) []Chunk {
	opts = opts.withDefaults()

	var out []Chunk
	for _, s := range sections(doc) {
		out = append(out, splitSection(doc.Lines, s, opts, len(out))...)
	}
	return out
}

// section is a heading and everything under it, until the next heading.
type section struct {
	start, end int // one-based, inclusive
	path       []string
}

// sections divides the document at its headings.
//
// Nesting is by heading level, so a chunk under "## Deload weeks" inside
// "# Training log" carries both — which is what turns a citation from "chunk
// 47" into something a person recognises.
func sections(doc parse.Document) []section {
	if len(doc.Lines) == 0 {
		return nil
	}

	body := max(doc.BodyStart, 1)
	if body > len(doc.Lines) {
		return nil
	}

	if len(doc.Headings) == 0 {
		return []section{{start: body, end: len(doc.Lines)}}
	}

	var (
		out   []section
		trail []parse.Heading
	)

	// Text before the first heading is its own section with no path: a preamble
	// belongs to the document, not to a section it precedes.
	if first := doc.Headings[0].Line; first > body {
		out = append(out, section{start: body, end: first - 1})
	}

	for i, h := range doc.Headings {
		for len(trail) > 0 && trail[len(trail)-1].Level >= h.Level {
			trail = trail[:len(trail)-1]
		}
		trail = append(trail, h)

		end := len(doc.Lines)
		if i+1 < len(doc.Headings) {
			end = doc.Headings[i+1].Line - 1
		}
		if end < h.Line {
			continue
		}
		out = append(out, section{start: h.Line, end: end, path: texts(trail)})
	}
	return out
}

func texts(hs []parse.Heading) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Text
	}
	return out
}

// splitSection emits one chunk per section, or several when it is too long.
func splitSection(lines []string, s section, opts Options, offset int) []Chunk {
	start, end, ok := trimBlank(lines, s.start, s.end)
	if !ok {
		return nil
	}

	if span(lines, start, end) <= opts.MaxChars {
		return []Chunk{newChunk(lines, start, end, s.path, offset+1)}
	}

	var out []Chunk
	for _, r := range splitRange(lines, start, end, opts) {
		out = append(out, newChunk(lines, r[0], r[1], s.path, offset+len(out)+1))
	}
	return out
}

// splitRange breaks an oversized range at paragraph boundaries, falling back to
// line boundaries for a paragraph that is itself too long.
//
// Ranges are returned rather than text, so a chunk's content is always a
// verbatim slice of the source and its line numbers cannot drift from it.
func splitRange(lines []string, start, end int, opts Options) [][2]int {
	var (
		out      [][2]int
		curStart = start
		curEnd   = start - 1 // empty
	)

	flush := func() {
		if curEnd >= curStart {
			if s, e, ok := trimBlank(lines, curStart, curEnd); ok {
				out = append(out, [2]int{s, e})
			}
		}
	}

	for _, p := range paragraphs(lines, start, end) {
		switch {
		case span(lines, p[0], p[1]) > opts.MaxChars:
			// A single paragraph over the bound: flush what is pending, then
			// cut it on line boundaries. Nothing smaller than a line is ever
			// split, so a chunk never begins mid-sentence.
			flush()
			out = append(out, splitLongParagraph(lines, p[0], p[1], opts)...)
			curStart, curEnd = p[1]+1, p[1]

		case curEnd < curStart:
			curStart, curEnd = p[0], p[1]

		case span(lines, curStart, p[1]) <= opts.MaxChars:
			curEnd = p[1]

		default:
			flush()
			curStart, curEnd = overlapStart(lines, curStart, curEnd, p[0], opts), p[1]
		}
	}
	flush()
	return out
}

func splitLongParagraph(lines []string, start, end int, opts Options) [][2]int {
	var out [][2]int
	for from := start; from <= end; {
		to := from
		for to < end && span(lines, from, to+1) <= opts.MaxChars {
			to++
		}
		if s, e, ok := trimBlank(lines, from, to); ok {
			out = append(out, [2]int{s, e})
		}
		if to >= end {
			break
		}
		from = max(overlapStart(lines, from, to, to+1, opts), from+1)
	}
	return out
}

// overlapStart backs the next chunk up over the tail of the previous one, so a
// claim split across the boundary is retrievable from either side.
func overlapStart(lines []string, prevStart, prevEnd, next int, opts Options) int {
	at := next
	for at-1 >= prevStart && span(lines, at-1, prevEnd) <= opts.OverlapChars {
		at--
	}
	return at
}

// paragraphs returns the blank-line-separated runs within a range.
func paragraphs(lines []string, start, end int) [][2]int {
	var (
		out [][2]int
		at  = -1
	)
	for i := start; i <= end; i++ {
		if strings.TrimSpace(lines[i-1]) == "" {
			if at >= 0 {
				out = append(out, [2]int{at, i - 1})
				at = -1
			}
			continue
		}
		if at < 0 {
			at = i
		}
	}
	if at >= 0 {
		out = append(out, [2]int{at, end})
	}
	return out
}

// trimBlank narrows a range past its leading and trailing blank lines.
//
// Narrowing the range rather than trimming the string is what keeps a chunk's
// content equal to the lines it claims. TrimSpace on the joined text would make
// every chunk that began after a blank line quote a range it does not fill.
func trimBlank(lines []string, start, end int) (int, int, bool) {
	for start <= end && strings.TrimSpace(lines[start-1]) == "" {
		start++
	}
	for end >= start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return start, end, start <= end
}

func span(lines []string, start, end int) int {
	total := 0
	for i := start; i <= end && i <= len(lines); i++ {
		total += len(lines[i-1]) + 1
	}
	return total
}

func newChunk(lines []string, start, end int, path []string, ordinal int) Chunk {
	content := strings.Join(lines[start-1:end], "\n")
	sum := sha256.Sum256([]byte(content))

	return Chunk{
		Ordinal:     ordinal,
		HeadingPath: append([]string(nil), path...),
		StartLine:   start,
		EndLine:     end,
		Content:     content,
		SHA256:      hex.EncodeToString(sum[:]),
	}
}
