package crews

import (
	"regexp"
	"strings"
)

// mentionPattern finds @handle where the @ starts a word, so an email
// address is not a mention. Handles follow internal/social: 3 to 20 of
// a-z, 0-9 and underscore.
var mentionPattern = regexp.MustCompile(`(?:^|[^\w@])@([A-Za-z0-9_]{3,20})\b`)

// parseMentions returns the handles body mentions, lower-cased, first
// occurrence order, without duplicates. all is true when body says
// @everyone, which only the owner and admins may use; the caller checks.
func parseMentions(body string) (handles []string, all bool) {
	seen := map[string]bool{}
	for _, m := range mentionPattern.FindAllStringSubmatch(body, -1) {
		h := strings.ToLower(m[1])
		if h == "everyone" {
			all = true
			continue
		}
		if !seen[h] {
			seen[h] = true
			handles = append(handles, h)
		}
	}
	return handles, all
}
