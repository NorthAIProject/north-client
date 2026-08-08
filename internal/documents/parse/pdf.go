package parse

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// KindPDF marks a document extracted from a PDF.
const KindPDF = "pdf"

// ErrNoText reports a PDF with nothing readable in it.
//
// Almost always a scan: a photograph of a page carries no text layer, and North
// does not do OCR. Its own error so the person can be told that specifically,
// rather than "this document produced nothing to search", which sounds like a
// bug in North rather than a property of their file.
var ErrNoText = errors.New("this PDF has no text in it — it looks like a scan, and North cannot read images yet")

// PDF extracts a PDF's text into the same shape as any other document.
//
// # What the line numbers mean
//
// A PDF has no lines. What comes back here is North's extraction of it, and
// Document.Lines — which every chunk's start_line and end_line index into — are
// lines of that extraction, not of the file. They are stable, because the
// extraction is deterministic, but they will not match what a PDF viewer shows.
//
// That is why each page gets a synthetic "Page N" heading: the heading path is
// what a person actually reads on a citation, and "Physio report › Page 3" is
// something they can act on, where "lines 82-96" is not.
func PDF(filename string, data []byte) (Document, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		// Encrypted, malformed, or not really a PDF. The detail is for the log;
		// what reaches the person is that North could not open it.
		return Document{}, fmt.Errorf("North could not open this PDF: %w", err)
	}

	var (
		lines    []string
		headings []Heading
	)

	for page := 1; page <= reader.NumPage(); page++ {
		p := reader.Page(page)
		if p.V.IsNull() {
			continue
		}

		text, err := p.GetPlainText(nil)
		if err != nil {
			// One unreadable page must not cost the whole document. The gap is
			// recorded where a reader will see it rather than silently skipped.
			text = "[North could not read this page]"
		}

		heading := fmt.Sprintf("Page %d", page)
		headings = append(headings, Heading{Level: 1, Text: heading, Line: len(lines) + 1})
		lines = append(lines, "# "+heading, "")

		lines = append(lines, splitExtracted(text)...)
		lines = append(lines, "")
	}

	if !hasText(lines) {
		return Document{}, ErrNoText
	}

	return Document{
		Kind:      KindPDF,
		Title:     titleFromFilename(filename),
		Lines:     lines,
		Headings:  headings,
		BodyStart: 1,
	}, nil
}

// splitExtracted turns one page's extracted text into lines.
//
// GetPlainText returns a page as a single run with the original line breaks
// mostly gone, so it is re-wrapped on sentence boundaries. Not cosmetic: the
// chunker splits on blank lines and line boundaries, and a page that is one
// enormous line can only be split arbitrarily — which is how a citation ends up
// quoting half a sentence.
func splitExtracted(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")

	var out []string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		if len(line) <= extractedLineWidth {
			out = append(out, line)
			continue
		}
		out = append(out, wrapSentences(line)...)
	}
	return out
}

// extractedLineWidth is where an extracted line is long enough to be worth
// breaking up. Roughly two printed lines of prose.
const extractedLineWidth = 160

func wrapSentences(line string) []string {
	var (
		out     []string
		current strings.Builder
	)

	flush := func() {
		if s := strings.TrimSpace(current.String()); s != "" {
			out = append(out, s)
		}
		current.Reset()
	}

	for _, word := range strings.Fields(line) {
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)

		endsSentence := strings.HasSuffix(word, ".") ||
			strings.HasSuffix(word, "!") ||
			strings.HasSuffix(word, "?")

		if endsSentence && current.Len() >= extractedLineWidth/4 {
			flush()
		} else if current.Len() >= extractedLineWidth {
			flush()
		}
	}
	flush()

	if len(out) == 0 {
		return []string{line}
	}
	return out
}

// hasText reports whether anything survived extraction beyond the synthetic
// page headings this package added itself.
func hasText(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "# Page ") {
			continue
		}
		if trimmed == "[North could not read this page]" {
			continue
		}
		return true
	}
	return false
}
