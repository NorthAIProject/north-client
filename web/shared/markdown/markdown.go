// Package markdown renders coach replies for display.
//
// Models write markdown whether or not you ask them to, so rendering it is the
// difference between a reply that reads as prose and one littered with
// asterisks. goldmark rather than a hand-rolled renderer: markdown has more
// edge cases than it looks, and getting escaping wrong here is a cross-site
// scripting hole.
package markdown

import (
	"bytes"
	"sync"

	"github.com/a-h/templ"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	once     sync.Once
	renderer goldmark.Markdown
)

func engine() goldmark.Markdown {
	once.Do(func() {
		renderer = goldmark.New(
			// Strikethrough, tables, and autolinks. Task lists are omitted:
			// a checkbox the user cannot tick is a worse affordance than a dash.
			goldmark.WithExtensions(extension.Strikethrough, extension.Table, extension.Linkify),
			goldmark.WithRendererOptions(
				// Raw HTML in the source is escaped, not passed through.
				//
				// Model output is untrusted: it reflects whatever the user typed
				// and whatever their uploaded documents contain. WithUnsafe here
				// would turn a coaching reply into a script injection vector.
				html.WithXHTML(),
			),
		)
	})
	return renderer
}

// Render converts markdown to HTML safe for insertion into a page.
//
// On failure the original text is returned escaped, because a reply the user
// can read as plain text is better than an empty bubble.
func Render(source string) templ.Component {
	var buf bytes.Buffer

	if err := engine().Convert([]byte(source), &buf); err != nil {
		return templ.Raw(templ.EscapeString(source))
	}

	// Safe: goldmark escaped everything dangerous, and unsafe rendering is off.
	return templ.Raw(buf.String())
}

// RenderString is Render for callers that need the HTML as a string, such as
// the SSE stream.
func RenderString(source string) string {
	var buf bytes.Buffer

	if err := engine().Convert([]byte(source), &buf); err != nil {
		return templ.EscapeString(source)
	}
	return buf.String()
}
