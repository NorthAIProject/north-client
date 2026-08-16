// Package messaging adapts messaging platforms onto the coach.
//
// North's premise is one brain, many mouths. This package is what makes that
// literally true: a message arriving from Telegram, Discord or WhatsApp is
// turned into the same coach.Service turn the web chat produces, with the same
// memory, the same context, the same tools and the same confirmation gate. No
// platform-specific business logic lives here or anywhere downstream — the
// platform stops at InboundMessage on the way in and Transport on the way out.
//
// # Layering
//
// This is infrastructure, in the sense ARCHITECTURE.md means: it coordinates,
// it does not decide. It builds no prompt, chooses no provider and knows what
// a tool is only well enough to ask a person about one. Everything it does
// beyond translation — metering, thread selection, confirmation — exists
// because the web handler does it too, and a second mouth that skipped them
// would be a second, weaker product.
//
// # Shared conversation identity
//
// A platform message arrives carrying a chat id and nothing else. Turning that
// into a North account is the whole identity problem, and the repository
// already had three shapes to choose between:
//
//   - auth_identities (00008) maps (provider, provider_subject) to a user and
//     is how Google sign-in works. It is a way to sign in.
//   - strava_connections (00022) maps a user to a provider's data, one way.
//     There is no reverse lookup, because nothing inbound ever needs one.
//   - agent_connections (20260813180000) maps a hashed bearer to a user, and
//     is how the MCP endpoint and health ingest authenticate.
//
// messaging_links takes auth_identities' shape — UNIQUE (platform,
// external_id) resolving to a user_id — because the question asked here is
// auth_identities' question. It is deliberately a separate table for the
// reason 00022 gives for keeping Strava out of it: this is not a way to sign
// in. If a messaging link were an identity, unlinking Telegram could lock
// somebody out of their account, and a chat id is a far weaker credential than
// anything that should be able to do that.
//
// The binding is established by a one-time code:
//
//  1. The person, already authenticated in the web app, asks Settings for a
//     code. Only the code's SHA-256 hash is stored, and issuing a new one
//     invalidates the last.
//  2. They send it to the bot. That message is the only thing an unlinked chat
//     is allowed to do — it never reaches the coach, so nobody who merely
//     finds the bot can spend a model budget.
//  3. Redemption is atomic and single use, rate limited per chat, and refuses
//     a chat already bound to a different account rather than moving it.
//
// The code is short because it is retyped on a phone. Its safety comes from
// the expiry, the single use and the rate limit rather than from its length —
// see CodeLength.
//
// # Shared conversation
//
// A linked person has one thread, not two. Handle continues the newest live
// chat conversation, which is the same one the web app is showing, so asking
// on the phone and asking in the browser continue each other. The alternative
// — a conversation per platform — would give the coach two half-histories and
// make it worse at the one thing it exists to be good at.
//
// # Delivery
//
// Adapters acknowledge an update before they answer it, because a coach turn
// can take minutes and every platform retries anything slower than a few
// seconds. The reply is generated on a detached context and pushed when it is
// ready. The cost is that a restart mid-generation loses the push; the reply
// is still persisted and still appears in the web thread, so what is lost is
// the notification rather than the answer. Redeliveries are made harmless by
// the last_update_id watermark on messaging_links.
package messaging
