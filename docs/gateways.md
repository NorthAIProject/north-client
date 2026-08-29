# Gateways

> A gateway is a way into North that is not a browser.
>
> This document is where each one is described: what it is, how it
> authenticates, and — importantly — how much of it has actually been verified
> against the real thing on the other side.

---

## What counts as a gateway

North's web app is one mouth on one brain. A gateway is any other mouth: a
messaging platform, an agent holding a token, a phone syncing health data in the
background.

They all share three properties, and those properties are what make them a
category rather than a list:

1. **The caller is not a browser.** No cookie, no form, no CSRF token. Session
   middleware would resolve a session that has nothing to do with the presented
   credential, and CSRF would reject every call with an HTML body the caller
   cannot read.
2. **They mount outside the session group.** Each gets its own chi group in
   `cmd/web/main.go`, with its own body cap sized to what it actually receives.
3. **They own no business logic.** A gateway translates and authenticates. The
   moment one starts deciding something, that decision belongs in a service.

`ARCHITECTURE.md` calls these "thin interfaces". This document is the
operational view of the same idea.

---

## The gateways today

| Gateway | Route | Authenticates with | Body cap |
|---|---|---|---|
| MCP | `POST /mcp` | Bearer token (`agent_connections`) | 1 MiB |
| Health ingest | `POST /ingest/health` | Bearer token (`agent_connections`) | 8 MiB |
| Telegram (webhook) | `POST /webhooks/telegram` | `X-Telegram-Bot-Api-Secret-Token` | 1 MiB |
| Telegram (polling) | none — outbound only | Bot token in the request path | n/a |

Each body cap is sized to its payload rather than copied from its neighbour. An
MCP call is a small JSON-RPC envelope; one health sync is a week of per-beat
samples and 1 MiB would reject an ordinary Monday morning.

---

## MCP

North exposes its coaching capabilities as MCP tools so an outside agent —
Claude Code, Codex, Hermes — can read goals, log check-ins and ask the coach.

- **Identity:** every token resolves to its own owner via
  `connections.Service.Authenticate`, which is what makes it safe to serve
  publicly at all. Compare `cmd/mcp-server`, where one static token maps to one
  configured account and the endpoint belongs on a tailnet.
- **Rate limited** per account, in memory, per process.
- **Origin checked:** browsers are rejected by default. A real MCP client sends
  no `Origin` header; a request that does is a web page, and a web page is not
  the intended caller.

Setup instructions live in `skills/north-connect/SKILL.md`.

---

## Health ingest

A background process on somebody's phone posting metrics, holding the same
revocable bearer token as MCP and carrying neither a cookie nor a CSRF token.

Two-stage throttle: one bound per token, one anonymous bound in front of it, so
an unauthenticated flood cannot spend the authenticated budget.

---

## Telegram

The first messaging gateway. Implemented; see `internal/messaging` for the
platform-agnostic adapter and `internal/messaging/telegram` for the transport.

**Setup is documented in the README** under *Setting up Telegram* — creating the
bot, choosing polling versus webhook, and linking an account. This section
covers only what belongs in a gateway register.

- **Two inbound modes, selected by configuration, never by a switch.** A webhook
  when `TELEGRAM_WEBHOOK_SECRET` is set, long polling when it is not. There is
  deliberately no combination that serves a webhook without a secret, because
  that would be an open endpoint.
- **Polling exists so the feature is developable.** A webhook needs a public
  HTTPS URL, which `localhost` does not have. Polling needs only an outbound
  connection. It does not scale past one process: two pollers on one bot each
  receive half the updates.
- **Identity is a one-time code**, not the chat id. See the *Shared conversation
  identity* section of `ARCHITECTURE.md`.
- **Private chats only.** A group has one chat id shared by everybody in it, so
  a linked group would hand every member the owner's account. Groups are refused
  and the bot removes itself.
- **Acknowledge, then answer.** A coach turn takes minutes and Telegram retries
  anything slower than seconds, so the update is acknowledged before it is
  answered and the reply is pushed when ready. A restart mid-generation loses
  the push, not the answer — that is still persisted and still in the web thread.

### Verification status

**Partly verified, 2026-08-29.** The Bot API has answered a real token and has
delivered a real message. `main telegram-check` ran `getMe`, `getWebhookInfo`,
`setMyCommands` and `sendMessage` against `@Khepri_app_Bot` successfully.

Outbound is therefore proven: North can reach a person on Telegram. What has
**not** happened is anything *inbound* — no person has redeemed a link code, no
message has travelled from a phone into the coach, and no generated reply has
been through the HTML parser on a real client. So the adapter is no longer
unproven, and it is not shipped either.

| Verified against the real API | Still only covered by tests |
|---|---|
| Token authenticates (`getMe`) | The link-code flow, end to end |
| Delivery mode matches the config (`getWebhookInfo`) | Inline keyboards on a real client |
| Command menu publishes (`setMyCommands`) | Whether real replies trip the HTML parser |
| **Outbound delivery to a real chat (`sendMessage`)** | Long-poll receiving a real update |
| Error envelope on a rejected token | Confirmation round-trip from a phone |

One bug came out of that first live run, and it is the kind no stand-in could
have caught: the bot token was being logged in plaintext. Telegram puts the token
in the URL path, and `net/http` wraps every *transport* failure in a `*url.Error`
whose `Error()` prints the whole URL — so any network-level failure wrote the
credential into the log. Fixed in `client.call` by unwrapping to the inner cause;
see `TestATransportFailureDoesNotLeakTheToken`. Every test until then had used an
`httptest` server, which always answers, so the transport path never failed.

The remaining tests run against an `httptest` stand-in. They prove the adapter is
correct; they cannot prove a person can use it.

**Closing the rest** needs one pass through the Part 2 flow in the README, from a
real phone, against a bot that belongs to this project. Then update this table.

#### `main telegram-check`

The preflight. Read-only by default, safe against production:

```
go run ./cmd/web telegram-check
```

It answers three questions and then says what it has *not* proven, which is the
point of it existing:

- **Whose token is this?** `getMe`, and a warning if `TELEGRAM_BOT_USERNAME`
  names a different bot than the token belongs to — a mismatch sends people from
  Settings to the wrong bot, which looks exactly like the link code being broken.
- **Where does Telegram think updates go?** `getWebhookInfo`, compared against
  what North is configured to do. The two modes are mutually exclusive at
  Telegram's end, and getting it wrong is silent in both directions:
  - webhook secret set but no webhook registered → nothing ever arrives;
  - no webhook secret but a webhook still registered → every poll is refused,
    because Telegram will not serve `getUpdates` while a webhook exists.
- **Are deliveries failing?** Pending update count and Telegram's own
  last-delivery error, which is usually the fastest answer to "why is nothing
  arriving".

Two flags write, and are off by default: `--register-commands` publishes the
command menu, `--send-to <chat id>` delivers a test message. Do not point either
at a bot belonging to another project.

---

## Adding a gateway

The seam is `messaging.Transport` for a messaging platform, or a plain
`http.Handler` for anything else. Either way:

1. **Mount it in its own group**, outside CSRF and `LoadUser`, with a body cap
   sized to its real payload.
2. **Authenticate to an account.** `connections.Service.Authenticate` already
   has the shape — a presented credential in, a `users.User` out. It is
   implemented twice already; a third is a third implementation, not a rewrite.
3. **Meter it.** A gateway that reaches the coach without spending
   `quota.CoachMessage` is a way around the limits rather than a second way in.
4. **Put no logic in it.** If the gateway starts deciding something, that
   decision belongs in a service where the web app can reach it too.
5. **Record its verification status here**, honestly. A gateway whose tests pass
   and which has never spoken to the real service is not the same thing as one
   that has, and the difference is invisible from a green build.

Discord and WhatsApp are a new package each under `internal/messaging/`, not an
edit to the existing one.
