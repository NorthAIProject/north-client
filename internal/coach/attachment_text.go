package coach

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Attachment kinds as conversations stores them. Spelled here rather than
// taken from internal/media, which this package must not import.
const (
	attachmentImage = "image"
	attachmentFile  = "file"
)

// maxAttachmentChars caps the text one document adds to a turn. About eight
// pages: a week of meals or a training block fits whole, and a long manual
// cannot crowd the person's own words and history out of the context window.
const maxAttachmentChars = 24_000

// truncatedNote follows a document cut at maxAttachmentChars. It names the
// import tool so the model does not conclude the rest of a plan is lost.
const truncatedNote = "[truncated; the full file is still available to import_plan_from_attachment]"

// ErrNoTextLayer is what an AttachmentTexter returns for a PDF whose text
// cannot be extracted. The file is not broken: import_plan_from_attachment
// sends the PDF itself to the model, so the note says that instead.
var ErrNoTextLayer = errors.New("this PDF has no text that can be extracted")

// noTextLayerNote is the note for such a PDF, naming the tool that can still
// read it.
func noTextLayerNote(name string) string {
	return "[" + name + " is a PDF whose text can't be extracted here; it can still be imported as a plan with import_plan_from_attachment]"
}

// closingAttachmentTag matches anything a model might read as the end of an
// attachment block: any letter case, and spaces either side of the slash.
var closingAttachmentTag = regexp.MustCompile(`(?i)<\s*/\s*attachment`)

// attachmentBlock wraps a document's text in the tags the system prompt
// tells the model to read as data, never as instructions.
//
// A closing tag inside the text, in any spelling, loses its "<", so a file
// cannot end its own block early and have what follows read as if the person
// wrote it.
func attachmentBlock(name, text string) string {
	truncated := false
	if utf8.RuneCountInString(text) > maxAttachmentChars {
		text = string([]rune(text)[:maxAttachmentChars])
		truncated = true
	}
	text = closingAttachmentTag.ReplaceAllString(text, "&lt;/attachment")

	var b strings.Builder
	b.WriteString(`<attachment name="` + name + `">` + "\n")
	b.WriteString(text)
	b.WriteString("\n</attachment>")
	if truncated {
		b.WriteString("\n" + truncatedNote)
	}
	return b.String()
}

// attachmentName is a filename safe to put inside the tag's quotes.
func attachmentName(name string) string {
	name = strings.TrimSpace(strings.NewReplacer(`"`, "'", "<", "", ">", "", "\n", " ", "\r", " ").Replace(name))
	if name == "" {
		return "file"
	}
	return name
}

// unreadableReason is why a document could not be read, in words fit for the
// model to repeat. A refusal meant for the person (planimport's "This PDF is
// password-protected.") is passed on as written; anything else could name
// storage keys or provider errors, so it is summarised.
func unreadableReason(err error) string {
	var fields apperr.FieldErrors
	if errors.As(err, &fields) && len(fields) > 0 && strings.TrimSpace(fields[0].Message) != "" {
		return strings.TrimSpace(fields[0].Message)
	}
	if apperr.Is(err, apperr.ErrNotFound) {
		return "the file is no longer available"
	}
	return "the file could not be opened"
}
