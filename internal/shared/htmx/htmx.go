package htmx

import (
	"net/http"
	"strings"
)

// IsRequest reports whether the request came from htmx rather than a plain
// form post. The no-JavaScript path must keep working alongside panel swaps.
func IsRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// TargetID reports the id of the element htmx is swapping into, without the
// tag name or a leading '#'.
//
// The header's format changed with htmx 4: htmx 2 sent a bare id, htmx 4 sends
// "tag#id" (for example "div#knowledge-search-results"). A handler comparing
// against the bare id stops recognising its own panel and quietly answers with
// a full page instead, which reads as a layout bug rather than a header one.
func TargetID(r *http.Request) string {
	target := strings.TrimSpace(r.Header.Get("HX-Target"))
	if i := strings.LastIndex(target, "#"); i >= 0 {
		return target[i+1:]
	}
	return target
}
