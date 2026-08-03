package chat

import "encoding/json"

// jsString renders a Go string as a JavaScript string literal, quotes included.
//
// Needed wherever user or model text is interpolated into an Alpine expression:
// a message containing a quote or a newline would otherwise break out of the
// literal and, with the right content, run as code. templ.JSONString exists but
// returns an error alongside the value, which cannot be used inline in a
// template attribute.
func jsString(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		// Marshalling a string cannot realistically fail; an empty literal is
		// the safe answer if it ever does.
		return `""`
	}
	return string(encoded)
}
