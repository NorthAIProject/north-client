// Package parse turns a document's bytes into lines, headings, and a title.
//
// It is deliberately shallow. North does not render Markdown here and does not
// try to understand it — it needs to know where the sections are, so that a
// retrieved passage can say which part of the document it came from, and it
// needs the lines kept exactly as written, so that a citation's line numbers
// mean something when someone opens the file.
package parse

import (
	"path/filepath"
	"strings"
)

// Kinds of source this package understands.
const (
	KindMarkdown  = "markdown"
	KindPlaintext = "plaintext"
)

// Heading is one section marker in a document.
type Heading struct {
	// Level is 1..6, as in Markdown.
	Level int

	Text string

	// Line is one-based, into Document.Lines.
	Line int
}

// Document is a parsed source, ready to chunk.
type Document struct {
	Kind  string
	Title string

	// Lines is the source split on newlines, with nothing trimmed. Chunk line
	// ranges index into this, so anything that alters it makes every citation
	// point somewhere else.
	Lines []string

	Headings []Heading

	// BodyStart is the first line after any frontmatter block, one-based.
	BodyStart int
}

// LineCount is how many lines the source had.
func (d Document) LineCount() int { return len(d.Lines) }

// Parse reads a source according to its filename and MIME type.
//
// Anything not recognised as Markdown is treated as plain text rather than
// rejected: a document with no headings still chunks, still ranks, and is still
// better in the index than absent from it.
func Parse(filename, mime, source string) Document {
	if isMarkdown(filename, mime) {
		return Markdown(filename, source)
	}
	return Plaintext(filename, source)
}

func isMarkdown(filename, mime string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown", ".mdown":
		return true
	}
	return strings.Contains(strings.ToLower(mime), "markdown")
}

// Plaintext parses a source with no structure to find.
func Plaintext(filename, source string) Document {
	lines := splitLines(source)
	return Document{
		Kind:      KindPlaintext,
		Title:     titleFromFilename(filename),
		Lines:     lines,
		BodyStart: 1,
	}
}

// Markdown parses headings, frontmatter, and a title.
func Markdown(filename, source string) Document {
	lines := splitLines(source)
	bodyStart, frontMatter := frontMatterBounds(lines)

	doc := Document{
		Kind:      KindMarkdown,
		Lines:     lines,
		BodyStart: bodyStart,
		Headings:  headings(lines, bodyStart),
	}

	doc.Title = firstNonEmpty(
		frontMatterTitle(lines, frontMatter),
		firstTopHeading(doc.Headings),
		titleFromFilename(filename),
	)
	return doc
}

// splitLines keeps the source exactly as written.
//
// A trailing newline would otherwise produce a final empty line that is not
// part of the document, and every line range covering the end of the file would
// be one longer than the text it claims to quote.
func splitLines(source string) []string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.TrimSuffix(source, "\n")
	if source == "" {
		return nil
	}
	return strings.Split(source, "\n")
}

// frontMatterBounds returns the first body line and the frontmatter's own
// range, both one-based; the range is empty when there is none.
func frontMatterBounds(lines []string) (bodyStart int, block [2]int) {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return 1, [2]int{}
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return i + 2, [2]int{2, i}
		}
	}
	// An unterminated block is not frontmatter; it is a document that happens
	// to start with a horizontal rule.
	return 1, [2]int{}
}

func frontMatterTitle(lines []string, block [2]int) string {
	if block[0] < 1 {
		return "" // no frontmatter
	}
	for i := block[0]; i <= block[1] && i <= len(lines); i++ {
		key, value, ok := strings.Cut(lines[i-1], ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "title") {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

// headings finds ATX (`## Heading`) and setext (underlined) headings.
//
// Fenced code is skipped. A shell snippet is full of lines beginning with `#`,
// and treating those as sections would shred the document into chunks named
// after comments.
func headings(lines []string, bodyStart int) []Heading {
	var (
		out     []Heading
		inFence bool
		fence   string
	)

	for i := bodyStart; i <= len(lines); i++ {
		line := lines[i-1]
		trimmed := strings.TrimSpace(line)

		if marker := fenceMarker(trimmed); marker != "" {
			switch {
			case !inFence:
				inFence, fence = true, marker
			case strings.HasPrefix(marker, fence):
				inFence, fence = false, ""
			}
			continue
		}
		if inFence {
			continue
		}

		if h, ok := atxHeading(trimmed, i); ok {
			out = append(out, h)
			continue
		}
		// Setext underlines the *previous* line, so it can only be a heading if
		// that line was ordinary text and is not already one.
		if level, ok := setextLevel(trimmed); ok && i-1 >= bodyStart {
			text := strings.TrimSpace(lines[i-2])
			if text != "" && !endsWithHeading(out, i-1) {
				out = append(out, Heading{Level: level, Text: text, Line: i - 1})
			}
		}
	}
	return out
}

func fenceMarker(trimmed string) string {
	for _, ch := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, ch) {
			return ch
		}
	}
	return ""
}

func atxHeading(trimmed string, line int) (Heading, bool) {
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return Heading{}, false
	}
	rest := trimmed[level:]
	if rest != "" && !strings.HasPrefix(rest, " ") {
		// "#hashtag" is a word, not a heading.
		return Heading{}, false
	}
	text := strings.TrimSpace(strings.TrimRight(rest, " #"))
	if text == "" {
		return Heading{}, false
	}
	return Heading{Level: level, Text: text, Line: line}, true
}

func setextLevel(trimmed string) (int, bool) {
	if len(trimmed) < 2 {
		return 0, false
	}
	switch {
	case strings.Trim(trimmed, "=") == "":
		return 1, true
	case strings.Trim(trimmed, "-") == "":
		return 2, true
	}
	return 0, false
}

func endsWithHeading(out []Heading, line int) bool {
	return len(out) > 0 && out[len(out)-1].Line == line
}

func firstTopHeading(hs []Heading) string {
	for _, h := range hs {
		if h.Level == 1 {
			return h.Text
		}
	}
	if len(hs) > 0 {
		return hs[0].Text
	}
	return ""
}

func titleFromFilename(filename string) string {
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.NewReplacer("-", " ", "_", " ").Replace(base)
	if base = strings.TrimSpace(base); base == "" {
		return "Untitled"
	}
	return base
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return "Untitled"
}
