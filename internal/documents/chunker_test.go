package documents_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/documents/parse"
)

const trainingLog = `---
title: Training log
author: someone
---

Notes I keep between sessions.

# Training log

The whole point is to notice patterns I would otherwise forget.

## Deload weeks

Every fourth week is lighter. Volume drops to about sixty percent and
intensity stays where it was.

### What went wrong in March

I skipped the deload and my left shoulder started clicking on overhead
pressing. It took six weeks to settle.

## Nutrition

I eat more on training days. Nothing complicated.

` + "```" + `sh
# this is a shell comment, not a heading
echo "# neither is this"
` + "```" + `

Back to prose after the code block.
`

// The invariant the whole package rests on. Everything else here — the
// citations, the settings page, the evidence refs stored on a reply — is only
// as trustworthy as this.
func TestChunkContentAlwaysQuotesItsOwnLineRange(t *testing.T) {
	for name, source := range map[string]string{
		"training log":  trainingLog,
		"no headings":   "Just a paragraph.\n\nAnd another one.\n",
		"one long line": strings.Repeat("word ", 4000),
		"blank padded":  "\n\n\n# Heading\n\n\n\nBody text.\n\n\n",
		"empty":         "",
		"crlf":          "# Heading\r\n\r\nBody with windows endings.\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			doc := parse.Parse("log.md", "text/markdown", source)
			chunks := documents.ChunkDocument(doc, documents.Options{})

			for _, c := range chunks {
				if c.StartLine < 1 || c.EndLine > len(doc.Lines) || c.EndLine < c.StartLine {
					t.Fatalf("chunk %d has an impossible range %d-%d over %d lines",
						c.Ordinal, c.StartLine, c.EndLine, len(doc.Lines))
				}

				want := strings.Join(doc.Lines[c.StartLine-1:c.EndLine], "\n")
				if c.Content != want {
					t.Errorf("chunk %d lines %d-%d do not quote their content:\n got: %q\nwant: %q",
						c.Ordinal, c.StartLine, c.EndLine, c.Content, want)
				}
			}
		})
	}
}

func TestChunkOrdinalsAreDenseAndOrdered(t *testing.T) {
	doc := parse.Parse("log.md", "text/markdown", trainingLog)
	chunks := documents.ChunkDocument(doc, documents.Options{})

	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}
	for i, c := range chunks {
		if c.Ordinal != i+1 {
			t.Errorf("chunk at index %d has ordinal %d", i, c.Ordinal)
		}
		if i > 0 && c.StartLine < chunks[i-1].StartLine {
			t.Errorf("chunk %d starts before its predecessor", c.Ordinal)
		}
	}
}

func TestHeadingPathCarriesTheTrail(t *testing.T) {
	doc := parse.Parse("log.md", "text/markdown", trainingLog)
	chunks := documents.ChunkDocument(doc, documents.Options{})

	var found bool
	for _, c := range chunks {
		if !strings.Contains(c.Content, "shoulder started clicking") {
			continue
		}
		found = true

		want := []string{"Training log", "Deload weeks", "What went wrong in March"}
		if len(c.HeadingPath) != len(want) {
			t.Fatalf("heading path = %v, want %v", c.HeadingPath, want)
		}
		for i := range want {
			if c.HeadingPath[i] != want[i] {
				t.Fatalf("heading path = %v, want %v", c.HeadingPath, want)
			}
		}
	}
	if !found {
		t.Fatal("the nested passage was never chunked")
	}
}

// A shell comment inside a fenced block is not a section. Treating it as one
// shreds a document into chunks named after its code.
func TestFencedCodeDoesNotCreateHeadings(t *testing.T) {
	doc := parse.Parse("log.md", "text/markdown", trainingLog)

	for _, h := range doc.Headings {
		if strings.Contains(h.Text, "shell comment") || strings.Contains(h.Text, "neither is this") {
			t.Errorf("a line inside fenced code became the heading %q", h.Text)
		}
	}
}

func TestFrontMatterTitleWins(t *testing.T) {
	doc := parse.Parse("some-file.md", "text/markdown", trainingLog)
	if doc.Title != "Training log" {
		t.Errorf("title = %q, want %q", doc.Title, "Training log")
	}
	if doc.BodyStart != 5 {
		t.Errorf("body starts at line %d, want 5 (after the frontmatter block)", doc.BodyStart)
	}
}

func TestTitleFallsBackToTheFilename(t *testing.T) {
	doc := parse.Parse("physio_notes-2026.txt", "text/plain", "no structure at all")
	if doc.Title != "physio notes 2026" {
		t.Errorf("title = %q", doc.Title)
	}
}

// Long documents must actually be split, or the chunk bound means nothing.
func TestOversizedSectionsAreSplit(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Long section\n\n")
	for i := range 200 {
		b.WriteString("Paragraph ")
		b.WriteString(strings.Repeat("x", 60))
		b.WriteString("\n\n")
		_ = i
	}

	doc := parse.Parse("long.md", "text/markdown", b.String())
	chunks := documents.ChunkDocument(doc, documents.Options{})

	if len(chunks) < 2 {
		t.Fatalf("a %d-character section produced %d chunks", len(b.String()), len(chunks))
	}
	for _, c := range chunks {
		// Overlap can push a chunk slightly over; a large multiple cannot be
		// explained by overlap and means the bound is not being applied.
		if len(c.Content) > documents.DefaultMaxChars*2 {
			t.Errorf("chunk %d is %d characters, far over the %d bound",
				c.Ordinal, len(c.Content), documents.DefaultMaxChars)
		}
	}
}

// Determinism is what makes reindexing cheap and old citations resolvable.
func TestChunkingIsDeterministic(t *testing.T) {
	doc := parse.Parse("log.md", "text/markdown", trainingLog)

	first := documents.ChunkDocument(doc, documents.Options{})
	second := documents.ChunkDocument(doc, documents.Options{})

	if len(first) != len(second) {
		t.Fatalf("chunk counts differ: %d then %d", len(first), len(second))
	}
	id := uuid.New()
	for i := range first {
		if first[i].SHA256 != second[i].SHA256 {
			t.Errorf("chunk %d hashed differently between runs", i+1)
		}
		a := documents.ChunkID(id, first[i].Ordinal, first[i].SHA256)
		b := documents.ChunkID(id, second[i].Ordinal, second[i].SHA256)
		if a != b {
			t.Errorf("chunk %d produced ids %s and %s", i+1, a, b)
		}
	}
}

func TestChunkIDChangesWithContent(t *testing.T) {
	id := uuid.New()
	if documents.ChunkID(id, 1, "aaa") == documents.ChunkID(id, 1, "bbb") {
		t.Error("edited content kept the previous chunk id")
	}
	if documents.ChunkID(id, 1, "aaa") == documents.ChunkID(id, 2, "aaa") {
		t.Error("a moved chunk kept its previous id")
	}
	if documents.ChunkID(id, 1, "aaa") == documents.ChunkID(uuid.New(), 1, "aaa") {
		t.Error("the same passage in two documents shares one id")
	}
}
