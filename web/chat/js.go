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

// chatRootData is the Alpine scope on #chat-root.
//
// status is the second line of the Muse header's name pill. It lives on the
// root rather than on the pill so the stream bridge (Stage B) can set it from
// anywhere inside the page, and it is seeded with the translated "Ready" so
// the pill reads correctly before Alpine boots and after every sse-close swap.
func chatRootData(ready string) string {
	return `{
		threads: false,
		status: ` + jsString(ready) + `,
		fit() {
			const vv = window.visualViewport
			if (!vv) return
			this.$el.style.height = vv.height + 'px'
		}
	}`
}
