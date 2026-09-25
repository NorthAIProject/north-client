package ai

import "strings"

// JSONReply returns the JSON a model meant to send.
//
// Providers that honour ResponseSchema return bare JSON, and that passes
// through untouched. Some do not: an agent gateway such as Hermes answers in
// chat form, wrapping the object in a ```json fence or a sentence of
// preamble. Rejecting those costs a retry at best and, after the retries, the
// whole request, although the object itself was fine. So this unwraps a fence,
// then falls back to the span from the first '{' to the last '}'. Anything
// still malformed fails to decode as before, and the caller's retry names it.
func JSONReply(text string) []byte {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		return []byte(s)
	}
	if start := strings.Index(s, "```"); start >= 0 {
		body := s[start+3:]
		// Drop the fence's language tag ("json") up to the end of that line.
		if nl := strings.IndexByte(body, '\n'); nl >= 0 {
			body = body[nl+1:]
		}
		if end := strings.Index(body, "```"); end >= 0 {
			body = body[:end]
		}
		if b := strings.TrimSpace(body); strings.HasPrefix(b, "{") || strings.HasPrefix(b, "[") {
			return []byte(b)
		}
	}
	if first, last := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}'); first >= 0 && last > first {
		return []byte(s[first : last+1])
	}
	return []byte(s)
}
