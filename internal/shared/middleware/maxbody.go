package middleware

import "net/http"

// MaxBody caps how many bytes a request body may contain.
//
// It is mounted before CSRF, because CSRF parses multipart bodies to find the
// token and would otherwise buffer an unbounded upload before anything had a
// chance to reject it. Reading past the limit fails the request rather than
// consuming the whole thing first.
//
// Handlers still apply their own, friendlier limits: this one exists to stop
// the process being filled by a request nobody asked for, not to produce a good
// error message.
func MaxBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
