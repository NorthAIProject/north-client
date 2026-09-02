# IOS.md — how the native iOS client would be built

> Written 2026-09-02, the day Web Push for nudges landed on
> `feat/web-push-nudges`. This is a decision record and a starting point, not a
> build order. **Nothing here is implemented.** Read the "gate" section before
> anything else: the whole document is conditional on a number that does not
> exist yet.
>
> `ARCHITECTURE.md` says how the server is built. `DOMAIN.md` says what it is
> about. This says how a phone would talk to it.

---

## The gate

A native app is a second product surface with its own release train, review
process, and platform rules. It earns its place only if one thing is true:
**a note on the lock screen brings people back.** That is the single capability
the installed PWA cannot test today, and Web Push was built precisely so it can
be tested before any of this is written.

Do not start Phase 1 until, with Web Push live for four weeks:

| Number | Read from | Threshold that justifies going native |
|---|---|---|
| Opt-in | `push_subscribed` / `onboarding_completed` | People say yes when asked at the right moment |
| Return | `nudge_opened{channel:push}` within 24h of `nudge_delivered{channel:push}` | Nudges are acted on, not swiped away |
| Retention split | week-4 retention, push-subscribed vs not | The subscribed cohort retains materially better |

If push is refused or ignored, a native app will not fix that, and this
document stays a document.

The operating rule still applies: no new surface until ten strangers have used
the current one.

---

## What the app is, and is not

The iOS app is a **client**. The server keeps every rule, every prompt, every
piece of memory, and every decision about what to say and when. The app owns
exactly the things a phone can do that a browser cannot:

1. **Sign in the way iOS users expect** — Sign in with Apple, Google, a magic
   link that opens the app, and passkeys through the system sheet.
2. **Receive nudges through APNs**, which is the whole reason for the gate.
3. **Read Apple Health** and push it to the ingest endpoint that already exists
   and has never met a real payload (`internal/health/health.go`, package note).
4. **Sit on the Home Screen with a widget** showing the streak and today's
   check-in.

Everything else — goals, check-ins, chat, training plans, knowledge, reports —
is the same product, rendered natively over the same services. No business
logic moves to Swift. If a feature needs a rule, the rule goes in a Go service
and the app calls it.

---

## The migration path: strangler, not rewrite

The web app stays. The native app grows around it in three phases, each of
which ships something a person can use.

### Phase 1 — the shell

Native sign-in, native push, native Health, and a web view for everything else.

The app is: four sign-in screens, an APNs registration, a HealthKit background
sync, and a `WKWebView` pointed at `/app` with the session injected. Every
existing page works unchanged because it is already responsive and already
installed as a PWA on people's phones.

What this phase proves: that APNs nudges and HealthKit data are worth having.
Both are things a web view cannot do, so the shell is not a stopgap — it is
the minimum app that delivers the two capabilities the gate is about.

### Phase 2 — the core loop, native

Today, check-in, the coach conversation, nudges, goals. These are the five
screens somebody opens daily, and the ones where a web view feels wrong: the
chat stream, the check-in form's sliders, the goal list's swipe actions.

This is where the JSON API is built, one slice at a time, next to the HTML
handlers it mirrors.

### Phase 3 — the rest

Training plan editor, care (habits, water, sleep), nutrition, knowledge,
reports, settings. Each moves when its web view becomes the thing people
complain about, and not before. Some may never move.

---

## Authentication

All four methods end in the same place: a row in the existing session store
(`internal/auth/session.go`, `SessionStore.Create`). The token that today goes
into an HttpOnly cookie goes into the Keychain instead and travels as
`Authorization: Bearer <token>`. Same table, same hashing, same lifetime, same
`RevokeAll` when the password changes.

### Bearer sessions on the server

- `auth.Middleware` gains a second way to find the session: `Authorization:
  Bearer` on any path under `/api/`. Cookie auth is **not** accepted there, so
  the CSRF middleware does not apply to the API and cannot be bypassed through
  it.
- `SessionStore.Metadata` gains a `Device string` (the model name the app
  reports) so `/app/settings/activity` can show "iPhone 16, signed in
  yesterday" and the person can revoke it.
- Every API sign-in answers the same JSON:

```json
{
  "token": "…opaque, shown once…",
  "expires_at": "2026-10-02T10:00:00Z",
  "user": { "id": "…", "email": "…", "display_name": "…", "timezone": "…", "needs_onboarding": false }
}
```

### Email and password

Already exists. `auth.Service.Signup` and `Login` take a `SignupInput` and a
`LoginInput` and return the raw session token; the HTML handlers set a cookie
with it. The JSON handlers return it instead.

| Method | Path | Body | Reuses |
|---|---|---|---|
| POST | `/api/v1/auth/signup` | `{email, password, display_name, timezone}` | `Service.Signup` |
| POST | `/api/v1/auth/login` | `{email, password}` | `Service.Login` |
| POST | `/api/v1/auth/logout` | — | `Service.Logout` |
| POST | `/api/v1/auth/forgot-password` | `{email}` | `Service.RequestPasswordReset` |

Password reset stays a web page: the email link opens `/reset-password` in
Safari, which is fine — it happens once a year.

### Magic link

Does not exist yet. It is the sign-in method that fits a phone best: no
password to type on glass, no third party in the loop, and it doubles as
signup for an address North has never seen.

Server side, it is the password reset flow with a different outcome:

- `magic_link_tokens(id, email, token_hash, code_hash, expires_at, used_at,
  created_at)`. Fifteen minutes, single use, hashed at rest, exactly as the
  reset token is. **Two secrets per row**: a long token for the link and a
  six-digit code for the case where the email is read on a different device
  from the one signing in (a laptop mailbox, an iPhone app). The code is
  rate-limited to five attempts per row.
- `POST /api/v1/auth/magic-link` with `{email}` always answers `202`, whether
  or not the address exists — the same non-disclosure the reset request keeps.
  Quota: a new `quota.Action` so one address cannot be flooded.
- The email carries `https://<BASE_URL>/auth/magic?token=…` and the code. Sent
  through the existing SMTP mailer (`internal/auth/mailer.go`).
- `POST /api/v1/auth/magic-link/exchange` with `{token}` **or** `{email, code}`
  consumes the row, finds or creates the account by email, and returns a
  session. Creating on first use is deliberate: a person who asked for a link
  has already proved they own the address, which is more than a password
  signup proves.
- The web app gets the same link handler at `GET /auth/magic` so a link opened
  where the app is not installed still signs the person in to the browser.

On the phone, the link is a **Universal Link**: iOS opens the app directly
because `/.well-known/apple-app-site-association` (served by the web binary,
see "Server changes") lists the path. The app reads the token from the URL and
calls exchange. If the app is not installed, the same URL falls through to the
web handler.

### Google

Already exists for the web as an OAuth code flow through `golang.org/x/oauth2`
(`internal/auth/google.go`), ending in `FindOrCreateGoogleUser(profile)`.

The phone does not bounce through Safari. It uses the Google Sign-In SDK,
which returns an **ID token** — a JWT signed by Google carrying `sub`, `email`,
`email_verified`, `name`. The server verifies it and reuses the same
find-or-create:

- `POST /api/v1/auth/google` with `{id_token}`.
- Verification: fetch Google's JWKS, check signature, `iss` is
  `https://accounts.google.com`, `exp`, and `aud` is one of the accepted client
  IDs. There will be two: the existing web client and a new **iOS OAuth
  client** in the same Google Cloud project (`GOOGLE_IOS_CLIENT_ID`). Google
  publishes `google.golang.org/api/idtoken` for exactly this; it is a small
  dependency and the alternative is hand-rolling JWKS caching.
- Then `FindOrCreateGoogleUser` with the profile out of the token. Same
  `auth_identities` row as a web sign-in, so a person who signed up on the
  web with Google lands in the same account on the phone.

Alternative considered and rejected: `ASWebAuthenticationSession` against the
existing `/auth/google`. It works, but it ends in a cookie, opens a system
sheet that says "wants to use google.com to sign in", and the redirect back
needs a custom scheme. The ID-token path is what the SDK is for.

### Apple

Does not exist and is **not optional**: App Store Review Guideline 4.8 requires
Sign in with Apple, or an equivalent privacy-preserving option, in any app that
offers a third-party sign-in like Google.

- On the phone: `AuthenticationServices`,
  `ASAuthorizationAppleIDProvider`. The request carries a **nonce**: the app
  generates a random string, sends its SHA-256 in the request, and sends the
  raw string to the server. The server checks the token's `nonce` claim is the
  hash of what it received. This is what stops a stolen identity token being
  replayed against North.
- Apple returns an identity token (JWT), an authorization code, and — **on the
  very first sign-in only** — the person's name and email. Later sign-ins carry
  neither, so the app must send them the first time or they are gone. The
  email may be a private relay address (`…@privaterelay.appleid.com`).
- `POST /api/v1/auth/apple` with `{identity_token, authorization_code, nonce,
  full_name?}`.
- Verification: Apple's JWKS at `https://appleid.apple.com/auth/keys`, `iss`
  is `https://appleid.apple.com`, `aud` is the app's bundle ID
  (`APPLE_BUNDLE_ID`), `exp`, nonce as above. `sub` is the stable identifier;
  the email is not (people can revoke the relay).
- Storage: `auth_identities` already exists with `(provider,
  provider_subject)` and is how Google is stored today
  (`FindOrCreateGoogleUser` in `internal/auth/google.go`). Apple is a second
  provider value in the same table; the find-or-link-by-email path is reused
  as is. No migration.
- **Private relay email.** Any mail North sends to a relay address — a magic
  link, a password reset — is dropped unless the sending domain is registered
  under *Certificates, Identifiers & Profiles → Sign in with Apple → Email
  Communication*. Register `SMTP_FROM`'s domain there before the first
  TestFlight build.
- **Revocation.** Two obligations. On launch the app calls
  `getCredentialState(forUserID:)` and signs out locally on `.revoked`. And
  when a person deletes their account, the server must revoke the Apple token
  with `POST https://appleid.apple.com/auth/revoke` (required since 2022 for
  apps offering account deletion). That call needs a **client secret JWT**
  signed with an Apple private key: `APPLE_TEAM_ID`, `APPLE_SIGNIN_KEY_ID`,
  `APPLE_SIGNIN_PRIVATE_KEY` (the `.p8`). Apple can also push
  server-to-server notifications when a person revokes from Settings; the
  endpoint for those is `POST /api/v1/auth/apple/notifications`, and it should
  sign the person out everywhere (`SessionStore.RevokeAll`).
- The web app can offer the same button later through Apple's JS SDK and a
  Services ID (`APPLE_SERVICE_ID`). Not needed for the app.

### Passkeys — free

`/auth/passkey/*` already speaks JSON (`internal/auth/webauthn.go`). On iOS,
`ASAuthorizationPlatformPublicKeyCredentialProvider` runs the same WebAuthn
ceremony with the same relying party ID, as long as the AASA file lists
`webcredentials`. A person who registered a passkey on the web signs in to the
app with Face ID and no new server code beyond returning the session as JSON
rather than a cookie.

### Account deletion — required, exists

Guideline 5.1.1(v): an app with account creation must offer account deletion
in the app. `POST /app/settings/account/delete` exists; the API form is
`DELETE /api/v1/account`. With Apple sign-in it must also make the revoke call
above.

---

## Push: APNs next to Web Push

`internal/push` was written so this is an addition, not a rewrite.

- `push_subscriptions` gains `platform text NOT NULL DEFAULT 'webpush'`. For
  APNs the `endpoint` column holds the device token and `p256dh`/`auth` are
  empty. One table, because "does this person have any device that can be
  reached" is one question.
- A second `Sender`: token-based APNs auth over HTTP/2 to
  `api.push.apple.com`, signed with an Apple key (`APNS_KEY_ID`,
  `APNS_TEAM_ID`, `APNS_PRIVATE_KEY`, topic = bundle ID). `APNS_ENVIRONMENT`
  selects sandbox for TestFlight-internal builds. `github.com/sideshow/apns2`
  is the usual library; the request is small enough to hand-roll if the
  dependency rule bites.
- Status mapping is the same shape: `410` or `BadDeviceToken` deletes the row;
  everything else stamps `failed_at`.
- Payload:

```json
{
  "aps": { "alert": { "title": "Check in with yourself", "body": "It has been 3 days." },
           "sound": "default", "thread-id": "/app/check-ins" },
  "nudge_id": "…", "href": "/app/check-ins"
}
```

- A tap calls `POST /api/v1/nudges/{id}/open` with `{channel: "apns"}` and
  navigates to `href`. `analytics.ChannelAPNs` joins `ChannelPush`; the
  retention question is answered per channel.
- `POST /api/v1/devices` with `{platform: "apns", token, device_name}`
  registers; `DELETE /api/v1/devices/{token}` on sign-out. The permission prompt
  happens at the same moment the dashboard offers it on the web — after a goal,
  a check-in, and a conversation — for the same reason: asked before the
  product has shown its value, the answer is no, permanently.

---

## Apple Health

`internal/health` is an ingest endpoint waiting for a client. The app is that
client.

- **Read**: heart rate, HRV (SDNN), VO2max, SpO2, resting heart rate, steps,
  body mass, body fat, sleep analysis (`HKCategoryType`), and workouts
  (`HKWorkout` with type, start, end, active energy). These map onto the
  `readings` and `workouts` arrays the endpoint already accepts.
- **Incremental**: `HKAnchoredObjectQuery` per type, anchors persisted in the
  app, so a sync sends only what is new. `enableBackgroundDelivery` plus an
  `HKObserverQuery` wakes the app when Health writes; `BGAppRefreshTask` is the
  fallback for a phone that has not opened the app.
- **Send**: `POST /ingest/health/apple_health` — the existing endpoint; the
  last path segment is the source. Body limit (8 MiB) and per-source
  throttling already exist there. One change: today it authenticates bearer
  tokens against `agent_connections` only (`Auth: connectionSvc` in
  `cmd/web/main.go`). The app holds a session token, so the handler's
  `Authenticator` becomes a composite that tries the connection store first
  and the session store second. Two lines, no new token kind, and a bridge
  app configured with a connection token keeps working.
- **Funnel**: `source_connected{source: healthkit}` when the first sync lands.
  This is the "first real signal" event and HealthKit is the cheapest source a
  person can connect: no OAuth, no other app, one permission sheet.
- The package note says field names and units are guesses. The first sync
  from a real phone will correct them; budget a day for that, not an hour.

---

## Endpoints the app consumes

The HTML routes are thin (parse, validate, call service, render). The JSON
routes are the same three lines with a different last one. They live **next to
the HTML handler in the same slice** (`internal/goals/api.go` beside
`internal/goals/handler.go`), share the service, and are mounted under
`/api/v1` in `cmd/web/main.go`. No separate API package, because the slice is
the unit of ownership and a handler that lives away from its service drifts.

Shared plumbing, once: `internal/shared/httpx` with `WriteJSON`, `ReadJSON`
(size-limited, as `internal/push/handler.go` does), and one `apperr` → status
mapping so every slice answers validation, not-found, and quota the same way.

### Phase 1 (shell)

| Purpose | Method + path | Notes |
|---|---|---|
| Sign in, all four ways | `/api/v1/auth/*` above | New |
| Session for the web view | `POST /api/v1/auth/web-session` | Returns nothing; sets the session cookie on the response so `WKWebView` inherits it. The token is the same value in both places, so this is a cookie write, not a second session |
| Register device | `POST /api/v1/devices` | New |
| Health sync | `POST /ingest/health/apple_health` | Exists; authenticator widened to accept session bearers |
| Who am I | `GET /api/v1/me` | New; `users.User` projection |
| Apple revoke webhook | `POST /api/v1/auth/apple/notifications` | New, unauthenticated, signature-checked |
| Universal links, passkeys | `GET /.well-known/apple-app-site-association` | New, static JSON, no auth |

### Phase 2 (core loop)

| Screen | Today (HTML) | JSON |
|---|---|---|
| Today | `GET /app`, `GET /app/panels` | `GET /api/v1/today` — the `dashboard.Snapshot`, serialised. Includes `next_step` so the app shows the same card |
| Check-in | `GET/POST /app/check-ins`, `PATCH/DELETE /app/check-ins/{id}` | `GET/POST /api/v1/check-ins`, `PATCH/DELETE /api/v1/check-ins/{id}` |
| Goals | `/app/goals`, `/app/goals/{id}`, `…/status`, `…/updates`, `…/milestones`, `…/milestones/{mid}/status` | Same paths under `/api/v1/goals`. Delete becomes `DELETE` |
| Coach | `POST /app/chat`, `POST /app/chat/{id}/messages`, `GET /app/chat/{id}/stream`, `GET …/resume`, `POST …/tools/{messageID}/{decision}`, `POST …/helpful` | Same shape under `/api/v1/chat`. The stream is the one endpoint that changes behaviour: see below |
| Nudges | `GET /app/nudges/bell`, `POST …/read`, `POST …/dismiss`, `GET …/open` | `GET /api/v1/nudges`, `POST /api/v1/nudges/{id}/read`, `…/dismiss`, `…/open` with `{channel}` |
| Notification prefs | `POST /app/settings/notifications` | `GET/PUT /api/v1/settings/notifications` |
| Profile, onboarding | `POST /app/settings/profile`, `/app/onboarding` | `PUT /api/v1/me`, `POST /api/v1/onboarding` |

**The chat stream.** `GET /app/chat/{id}/stream` is SSE whose `token` events
carry HTML fragments (`chatpages.TokenHTML`) for htmx to swap. A native client
wants text. The handler already has a `writeEvent(w, rc, name, data)` seam;
the JSON variant is the same loop emitting `{"text": "…"}` for `token`,
`{"message_id": …, "name": …, "args": …}` for a pending tool call, and the
same `error` and `done` events. Selected by `Accept: application/json`, or by
mounting the same handler under `/api/v1` with a different renderer — the
second is cleaner. On the phone, `URLSession.bytes(for:)` with `.lines` reads
it; no SSE library needed.

### Phase 3 (the rest)

| Slice | HTML today | JSON |
|---|---|---|
| Training plans | `/app/training/*` incl. `days/{day}/exercises/{index}/{swap,remove,move,sets}` | Same under `/api/v1/training`; these are already form-shaped primitives on `internal/workouts` |
| Care | `/app/care/{habits,water,sleep,reminders}` | `/api/v1/care/*` |
| Nutrition | `/app/nutrition/*` | `/api/v1/nutrition/*` |
| Memories | `/app/memories/*` | `/api/v1/memories/*` |
| Knowledge | `/app/knowledge`, `…/search`, `…/passages`, `…/{id}` | `/api/v1/knowledge/*`; upload is multipart, same as web |
| Form check | `/app/form`, `/app/form/{id}`, `…/status`, `/app/media/{id}` | `/api/v1/form/*`; video from `PhotosUI` / `AVFoundation` |
| Reports | `/app/reports/*` | `/api/v1/reports/*` |
| Fitness (Strava) | `/app/fitness/strava/connect` | Stays a web page opened in `ASWebAuthenticationSession`; it is OAuth and the callback lands on the server |
| Connections, AI keys, Telegram, calendar | `/app/settings/*` | Stay in the web view. Settings pages are the last thing worth rebuilding |
| Export, delete | `GET /app/settings/export.zip`, `POST …/account/delete` | `GET /api/v1/account/export.zip`, `DELETE /api/v1/account` |

Quota applies to the API exactly as to the web: `quota.Identity` is built from
the bearer session's user and tier, and the same middleware guards the same
actions.

---

## Apple frameworks and SDKs

Listed by the phase that first needs them. Nothing on this list is speculative;
each is named because a specific endpoint above needs it.

| Framework / SDK | For | Phase |
|---|---|---|
| **SwiftUI**, Observation, Swift Concurrency | The app. `@Observable` models over `URLSession`, no ViewModel layer for a client this thin | 1 |
| **AuthenticationServices** | Sign in with Apple (`ASAuthorizationAppleIDProvider`), passkeys (`ASAuthorizationPlatformPublicKeyCredentialProvider`), and `ASWebAuthenticationSession` for Strava | 1 |
| **CryptoKit** | SHA-256 of the Apple nonce | 1 |
| **Security** (Keychain) | The session token. `kSecAttrAccessibleAfterFirstUnlock` so background Health sync can authenticate | 1 |
| **GoogleSignIn-iOS** (SPM, third party) | The Google ID token. The one non-Apple dependency on the phone; justified because the alternative is a browser bounce | 1 |
| **UserNotifications** + APNs registration | `UNUserNotificationCenter` permission, `didRegisterForRemoteNotificationsWithDeviceToken`, tap handling → `/open` | 1 |
| **HealthKit** | `HKAnchoredObjectQuery`, `HKObserverQuery`, `enableBackgroundDelivery`, `HKWorkout`. Entitlement plus `NSHealthShareUsageDescription`. Read-only; North never writes to Health | 1 |
| **BackgroundTasks** | `BGAppRefreshTask` as the fallback Health sync trigger | 1 |
| **WebKit** | `WKWebView` for everything not yet native, with the session cookie set in its `WKWebsiteDataStore` | 1 |
| Associated Domains entitlement | `applinks:` for magic links, `webcredentials:` for passkeys | 1 |
| **WidgetKit** | Streak and "checked in today" on the Home Screen; reads `GET /api/v1/today` through an App Group cache | 2 |
| **App Intents** | "Check in", "Log water", "Ask Khepri" as Shortcuts and Siri; each is one API call | 2 |
| **ActivityKit** | Live Activity for the workout session timer (`/app/activity/*`) | 3 |
| **PhotosUI**, **AVFoundation** | Picking and trimming a form-check video before upload | 3 |
| **StoreKit 2** | See "Pro on the App Store" | Blocked |

Not needed: Core Data or SwiftData (the server is the store; the app caches
JSON), MapKit, CoreLocation, any analytics SDK (the funnel is server-side and
`coach_replied{surface: ios}` is one string).

---

## Server changes, in order

Everything below is Go and can be built and tested before a line of Swift
exists, which is the point of writing it down.

1. **Bearer sessions**: `auth.Middleware` reads `Authorization: Bearer` under
   `/api/`; `Metadata.Device`; JSON sign-in/sign-out handlers for email and
   password.
2. **`httpx`**: JSON read/write and the `apperr` status mapping.
3. **AASA**: `GET /.well-known/apple-app-site-association` from
   `APPLE_TEAM_ID` and `APPLE_BUNDLE_ID`, `Content-Type: application/json`, no
   auth, served by the web binary next to `web/pwa`.
4. **Magic link**: table, quota action, request and exchange endpoints, web
   `GET /auth/magic`, mail template.
5. **Apple**: verifier with JWKS cache; `apple` as a second
   `auth_identities` provider; `POST /api/v1/auth/apple`; revoke on delete;
   notifications webhook.
6. **Google ID token**: verifier, `GOOGLE_IOS_CLIENT_ID`, `POST
   /api/v1/auth/google`.
7. **APNs**: `platform` column, sender, `ChannelAPNs`, devices endpoints.
8. **`GET /api/v1/me`, `GET /api/v1/today`**, `POST /api/v1/auth/web-session`.
   End of Phase 1.
9. Phase 2 slices, one at a time, each with its `api.go` and a contract test
   in the style of `internal/mcpserver/contract_test.go` (a golden file of the
   JSON shapes, so a field rename fails the build rather than the app).

New environment, all optional, all documented in `docs/env-hosting.md` when
built:

```
APPLE_BUNDLE_ID=
APPLE_TEAM_ID=
APPLE_SIGNIN_KEY_ID=
APPLE_SIGNIN_PRIVATE_KEY=       # .p8, for the revoke client secret
APPLE_SERVICE_ID=               # web Sign in with Apple, later
APNS_KEY_ID=
APNS_PRIVATE_KEY=               # .p8; may be the same key as sign-in
APNS_ENVIRONMENT=sandbox|production
GOOGLE_IOS_CLIENT_ID=
```

Deployment does not change: same image, same two releases, same `AUTO_MIGRATE=false`
and PreSync migration. The API is the web binary. The worker sends APNs the
way it sends Web Push today.

---

## Pro on the App Store

The landing page sells Pro at €15/month and `main tier` grants it by hand.
Inside an iOS app, a subscription that unlocks features **must** go through
in-app purchase (Guideline 3.1.1); linking out to the web checkout is allowed
only in narrow reader-app cases that do not apply here.

Two honest options:

- **Do not sell in the app.** No upgrade button, no mention of Pro. Tier
  changes happen on the web or by hand as today. Apple permits an app that
  simply reflects an entitlement bought elsewhere as long as it does not point
  at where to buy it. This is the right answer until there is a web checkout
  worth mirroring.
- **StoreKit 2 + App Store Server Notifications v2**: `POST
  /api/v1/billing/apple/notifications` grants and revokes the tier from
  Apple's signed payloads. Roughly the same work as Stripe, plus Apple's 15–30%.

Decide when Stripe is decided. Not before.

---

## App Store review checklist

Things that get a build rejected, all of which have an answer above:

- Sign in with Apple is present because Google is (4.8).
- Account deletion is in the app and revokes the Apple token (5.1.1).
- `PrivacyInfo.xcprivacy` declares HealthKit, UserDefaults, and the reasons.
- `NSHealthShareUsageDescription` says what North reads and why, in the
  person's words, not the framework's.
- Health data is never used for advertising or sold; the privacy page says so.
- The notification permission is requested after value is shown, not at
  launch.
- No Pro upsell inside the app until IAP exists.
- Export and delete are reachable from the app (web view is acceptable).
- Nothing in the app requires an account to read the marketing pages; sign-in
  gates `/app`, which is the same rule the web enforces.

---

## What is deliberately not here

- **Android.** Health Connect is on-device only, Google Fit's REST API is
  going away, and there is no Sign in with Apple obligation. Everything in the
  API section applies unchanged; nothing in the Apple section does. Write
  `ANDROID.md` when the iOS numbers say to.
- **Offline.** The PWA caches a shell and nothing else, for reasons in
  `web/pwa/sw.js`. The app inherits the rule: no offline check-ins, no queued
  chat. A phone with no network shows the last `today` and says so.
- **A design system for iOS.** The web has templUI. The app uses system
  components and the platform's own conventions; the mascot is the one shared
  visual. The repository's SwiftUI skills (`swiftui-design-principles`,
  `swiftui-pro`) are the reference when the time comes.
- **MCP from the phone.** The MCP server is for agents. The app talks to the
  API. If a tool and an endpoint ever need the same shape, the service is
  where they meet, not the transport.
