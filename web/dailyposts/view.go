// Package dailyposts renders /app/daily.
package dailyposts

import (
	"strings"

	"github.com/NorthAIProject/north-client/internal/dailyposts/post"
)

// View is the page: today's post, when there is one, and the ones before it.
type View struct {
	Today   *post.Post
	Earlier []post.Post
}

// paragraphs splits a body on blank lines. The model writes plain text, and
// a paragraph per element is what keeps it readable without markdown.
func paragraphs(body string) []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// draft is what "Ask the coach" puts in the composer.
func draft(p post.Post) string {
	return "About today's post, \"" + p.Title + "\": "
}
