// Package live holds the names the self-refreshing check-in displays agree
// on: the event a save announces and the trigger every display listens with.
//
// It is its own package because the check-ins page, the overview and My Day
// all draw check-in state and none of them should import another to share two
// strings.
package live

// CheckInSaved is the event a check-in save, edit or delete announces through
// the HX-Trigger response header. The server aims it at body.
const CheckInSaved = "checkin-saved"

// CheckInTrigger is the hx-trigger every live check-in display uses: refresh
// at once after a save in this tab, and poll every 20 seconds for saves made
// in another tab, the app, Siri or the coach. The poll is cheap because each
// display sends the version it is showing and gets 204 No Content, which
// htmx does not swap, when nothing has moved.
const CheckInTrigger = CheckInSaved + " from:body, every 20s"

// SavedHeader is the HX-Trigger value that announces a check-in save. It is
// aimed at body because the element that made the request has usually been
// swapped out by the time htmx fires it, and an event on a detached element
// reaches no listener.
const SavedHeader = `{"` + CheckInSaved + `":{"target":"body"}}`
