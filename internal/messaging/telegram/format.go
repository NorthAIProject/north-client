package telegram

import (
	"regexp"
	"strconv"
	"strings"
)

// Converting the coach's markdown for Telegram.
//
// The web renders replies as markdown, and models emit **bold** and bullet
// lists whether or not they are asked to. Sent to Telegram with no parse_mode
// those arrive as literal asterisks, which reads as a bug.
//
// web/shared/markdown cannot be reused. It emits full HTML, and Telegram
// accepts only a short list of tags — b, i, u, s, a, code, pre, blockquote,
// tg-spoiler. Anything else is not ignored but rejected, and the rejection
// takes the whole message with it. So this is a deliberately small converter
// covering what a coach actually writes, and everything else is escaped.
//
// This lives in the adapter rather than in messaging because it is transport
// formatting. Nothing here knows what a goal is.

var (
	// Fenced code first, so nothing inside one is treated as markup.
	fencedCode = regexp.MustCompile("(?s)```[a-zA-Z0-9]*\\n?(.*?)```")
	inlineCode = regexp.MustCompile("`([^`\n]+)`")

	// Bold before italic: ** would otherwise be read as two single markers.
	boldMarkdown = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)

	// Italic requires a non-space next to the marker, so "3 * 4" is arithmetic
	// rather than an unclosed emphasis.
	italicStar       = regexp.MustCompile(`\*([^*\s][^*\n]*?)\*`)
	italicUnderscore = regexp.MustCompile(`_([^_\s][^_\n]*?)_`)

	headingMarkdown = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	bulletMarkdown  = regexp.MustCompile(`(?m)^[ \t]*[-*][ \t]+`)
	linkMarkdown    = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)

	// A URL, so it can be set aside before the emphasis passes run.
	//
	// Underscores are ordinary in a URL and a marker in markdown, and
	// italicUnderscore ran first: https://youtu.be/a_b_c came out as
	// https://youtu.be/a<i>b</i>c, a link to nowhere. Wrapping it in [](…) did
	// not help, because linkMarkdown runs after the emphasis passes and by then
	// the address was already broken.
	//
	// Parentheses and brackets are excluded so that the address inside a
	// markdown link is matched without its surrounding punctuation, leaving
	// [label](<sentinel>) for linkMarkdown to find later.
	urlPattern = regexp.MustCompile(`https?://[^\s<>()\[\]]+`)
)

// trailingPunctuation is dropped from a matched URL and put back after it. A
// sentence ending "see https://kheprios.com." should not link the full stop.
const trailingPunctuation = ".,;:!?"

// stashURLs replaces every URL with a placeholder, returning the addresses in
// the order they were found.
//
// The placeholder uses the same \x00 sentinel shape as the code stash: it
// cannot occur in prose, it survives escapeHTML untouched, and it contains
// neither ')' nor whitespace, so linkMarkdown still matches around it.
func stashURLs(text string) (string, []string) {
	var urls []string

	replaced := urlPattern.ReplaceAllStringFunc(text, func(match string) string {
		address := strings.TrimRight(match, trailingPunctuation)
		urls = append(urls, address)
		return "\x00URL" + strconv.Itoa(len(urls)-1) + "\x00" + match[len(address):]
	})

	return replaced, urls
}

// restoreURLs puts the addresses back, passing each through transform first.
func restoreURLs(text string, urls []string, transform func(string) string) string {
	for i, address := range urls {
		text = strings.ReplaceAll(text, "\x00URL"+strconv.Itoa(i)+"\x00", transform(address))
	}
	return text
}

// markdownToHTML renders a coach reply as the HTML subset Telegram accepts.
func markdownToHTML(text string) string {
	// Code is extracted before anything else and put back last, so markers
	// inside a code span stay literal — `**not bold**` is an instruction, not
	// emphasis.
	var code []string
	stash := func(html string) string {
		code = append(code, html)
		// A placeholder that cannot appear in prose and survives escaping.
		return "\x00CODE" + strconv.Itoa(len(code)-1) + "\x00"
	}

	text = fencedCode.ReplaceAllStringFunc(text, func(m string) string {
		body := fencedCode.FindStringSubmatch(m)[1]
		return stash("<pre>" + escapeHTML(strings.TrimRight(body, "\n")) + "</pre>")
	})
	text = inlineCode.ReplaceAllStringFunc(text, func(m string) string {
		return stash("<code>" + escapeHTML(inlineCode.FindStringSubmatch(m)[1]) + "</code>")
	})

	// URLs go the same way as code, and for the same reason: the emphasis
	// passes below would otherwise read the underscores in an address as
	// markers. Set aside after code so an address inside a code span is left
	// alone, and restored after linkMarkdown so [label](…) still resolves.
	text, urls := stashURLs(text)

	// Everything that is not markup is escaped before any tag is introduced,
	// so a reply containing "5 < 7" cannot break the parser.
	text = escapeHTML(text)

	text = headingMarkdown.ReplaceAllString(text, "<b>$1</b>")
	text = boldMarkdown.ReplaceAllString(text, "<b>$1</b>")
	text = italicStar.ReplaceAllString(text, "<i>$1</i>")
	text = italicUnderscore.ReplaceAllString(text, "<i>$1</i>")
	text = bulletMarkdown.ReplaceAllString(text, "• ")
	text = linkMarkdown.ReplaceAllString(text, `<a href="$2">$1</a>`)

	// Escaped on the way back in: an address skipped the escapeHTML pass above,
	// and a bare '&' between query parameters would otherwise reach Telegram's
	// parser as the start of an entity.
	text = restoreURLs(text, urls, escapeHTML)

	for i, html := range code {
		text = strings.ReplaceAll(text, "\x00CODE"+strconv.Itoa(i)+"\x00", html)
	}
	return text
}

// stripMarkdown renders a reply as plain text, for the retry after Telegram
// refuses formatted markup.
//
// It removes the markers rather than the words: somebody reading "that is three
// sessions" has the answer, and that is the whole job of the fallback.
func stripMarkdown(text string) string {
	text = fencedCode.ReplaceAllString(text, "$1")
	text = inlineCode.ReplaceAllString(text, "$1")

	// Same reason as markdownToHTML: the emphasis passes below would eat the
	// underscores out of an address. This path matters more, not less — it is
	// the fallback used after Telegram has already refused the formatted
	// version, so it is the last chance the link has.
	text, urls := stashURLs(text)

	text = headingMarkdown.ReplaceAllString(text, "$1")
	text = boldMarkdown.ReplaceAllString(text, "$1")
	text = italicStar.ReplaceAllString(text, "$1")
	text = italicUnderscore.ReplaceAllString(text, "$1")
	text = bulletMarkdown.ReplaceAllString(text, "• ")
	// The address is kept: a link whose target vanished is worse than a clumsy
	// one, because there is no way to ask for it back.
	text = linkMarkdown.ReplaceAllString(text, "$1 ($2)")

	// No escaping here: this path sends without parse_mode, so the text is
	// delivered literally and an escaped '&' would show up as "&amp;".
	text = restoreURLs(text, urls, func(address string) string { return address })

	return strings.TrimSpace(text)
}

// escapeHTML covers the three characters Telegram's parser reacts to. It is
// deliberately not html.EscapeString, which also rewrites quotes and would turn
// an ordinary apostrophe into &#39; in the middle of a sentence.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
