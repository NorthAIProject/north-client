package coach

import (
	"context"
	"net/url"
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// maxSources bounds the list under a reply. A reply citing more pages than
// this is rare, and a list longer than the answer above it helps nobody.
const maxSources = 6

// addSources appends the pages not already listed.
func addSources(have, more []ai.Source) []ai.Source {
	seen := make(map[string]bool, len(have))
	for _, s := range have {
		seen[s.URL] = true
	}
	for _, s := range more {
		if !seen[s.URL] {
			seen[s.URL] = true
			have = append(have, s)
		}
	}
	return have
}

// sourceList is the web pages a reply cited, as Markdown to put under it.
// Empty when there are none worth listing.
//
// It is text in the reply rather than a field beside it so that every client
// shows it: the web app, the iOS builds already on phones, and Telegram all
// render a reply's text and nothing else.
//
// Titles and addresses come from the web, so neither is trusted. Only http
// and https addresses are listed, and a title cannot close its own link.
func sourceList(ctx context.Context, cited []ai.Source) string {
	var b strings.Builder
	listed := 0
	for _, s := range cited {
		if listed == maxSources {
			break
		}
		address, ok := webAddress(s.URL)
		if !ok {
			continue
		}
		title := linkText(s.Title)
		if title == "" {
			title = linkText(hostOf(address))
		}
		if listed == 0 {
			b.WriteString("\n\n**" + i18n.T(ctx, "chat.sources") + "**\n")
		}
		b.WriteString("- [" + title + "](" + address + ")\n")
		listed++
	}
	return strings.TrimRight(b.String(), "\n")
}

// webAddress returns s as a link target, if it is an http or https address.
// Brackets are escaped so the address cannot end the Markdown link early.
func webAddress(s string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	address := u.String()
	address = strings.ReplaceAll(address, "(", "%28")
	address = strings.ReplaceAll(address, ")", "%29")
	address = strings.ReplaceAll(address, " ", "%20")
	return address, true
}

func hostOf(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

// linkText makes a page title safe as the text of a Markdown link: one line,
// no brackets, and not long enough to wrap a phone screen three times.
func linkText(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	title = strings.NewReplacer("[", "(", "]", ")", "\\", "", "`", "'", "*", "", "_", " ", "<", "", ">", "").Replace(title)
	title = strings.Join(strings.Fields(title), " ")
	const limit = 90
	if r := []rune(title); len(r) > limit {
		title = strings.TrimSpace(string(r[:limit])) + "…"
	}
	return title
}
