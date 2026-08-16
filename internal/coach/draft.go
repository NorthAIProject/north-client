package coach

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const draftRuneCap = 500

func conversationURL(id uuid.UUID, draft string) string {
	path := "/app/chat/" + id.String()
	draft = clipDraft(draft)
	if draft == "" {
		return path
	}
	return path + "?draft=" + url.QueryEscape(draft)
}

func clipDraft(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= draftRuneCap {
		return s
	}
	return string([]rune(s)[:draftRuneCap])
}
