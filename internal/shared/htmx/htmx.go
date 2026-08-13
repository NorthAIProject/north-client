package htmx

import "net/http"

// IsRequest reports whether the request came from htmx rather than a plain
// form post. The no-JavaScript path must keep working alongside panel swaps.
func IsRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
