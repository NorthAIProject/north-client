package chat

import (
	"html"
	"strings"
)

// TokenHTML wraps one streamed token for insertion into the open bubble.
//
// The token is HTML-escaped. Model output is untrusted input: it reflects
// whatever the user typed and whatever their documents contain, so a reply
// containing <script> must render as text rather than execute.
//
// It is emitted as a bare escaped string rather than an element, so tokens
// concatenate into flowing text instead of a chain of nested spans.
func TokenHTML(token string) string {
	return html.EscapeString(token)
}

// StreamErrorHTML renders a failure inside the bubble the user is watching.
func StreamErrorHTML(message string) string {
	var b strings.Builder
	b.WriteString(`<p class="text-destructive text-sm">`)
	b.WriteString(html.EscapeString(message))
	b.WriteString(`</p>`)
	return b.String()
}
