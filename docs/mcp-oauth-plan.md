# Phase 3 — OAuth for the MCP endpoint

> **Superseded 2026-09-07.** Built. `internal/mcpauth` is the authorization
> server, `web/mcpauth` is the consent screen, and `/mcp`'s 401 now carries a
> `resource_metadata` pointer. The four open decisions this document ends on
> are answered at the bottom, along with the scoreboard the work is judged by.
>
> **The recommendation below was overridden, and the reason matters.** It says
> not yet, because this is supply-side work and the measurement that matters is
> the strangers count. That was correct for the flow described here, where the
> user is *already signed in* before consent (see "The flow OAuth replaces it
> with", below): under that assumption OAuth only removes friction for people
> who are already users, which is polish.
>
> What was built inverts it. **The consent screen creates the account.** That
> makes it the top of the funnel rather than a step after it — one URL in a
> tweet is simultaneously the advertisement, the signup and the install — and
> that is the only reason it was worth building before the strangers count
> moved. If the `account_created` row of the scoreboard comes back near zero,
> this document was right and it was polish after all.
>
> Three departures from the design here, each recorded where it applies:
> `issuance` is its own column rather than a fifth `client_kind`; refresh
> tokens are hashed rather than sealed; and one `agent_connections` row is one
> *grant* rather than one access token.

Written 2026-08-14, after phases 1 and 2 shipped. This was a decision record
and a starting point, not a build order — everything below the superseding note
is as it was written then, when nothing here was implemented. It is kept in the
present tense it was written in, because the reasoning is what the note above
is arguing with.

Read `docs/byok-plan.md` first if you want the reasoning behind the outbound
half; this document is only about the inbound one.

## What phases 1 and 2 left standing

North issues a personal access token from `/app/settings/connections`. The user
copies it into their agent's configuration, or pastes a generated prompt and
lets the agent edit its own config. `/mcp` on the web app authenticates each
token against `agent_connections` and acts as its owner.

That works, and every path through it ends the same way: *now paste this into a
file, or into an agent you already have running*. Someone who has never opened
`.mcp.json` stops there. That is the entire problem phase 3 solves, and it is
worth being precise that it solves nothing else — the tools, the data, and the
per-user isolation are already done.

The flow OAuth replaces it with: the user pastes **one URL** into their client's
"add a custom connector" field. The client discovers where to authorise,
registers itself, opens a browser, the user (already signed in to North) clicks
Approve, and the client stores a token the user never sees. No file, no secret
in the clipboard, no `${VAR}` indirection to explain.

## What has to be built

Five endpoints and a consent screen. The specifications move, so check them
against the MCP revision being targeted rather than trusting this list:

| Piece | Spec | What it does |
| --- | --- | --- |
| `/.well-known/oauth-protected-resource` | RFC 9728 | Tells a client which authorization server guards `/mcp` |
| `/.well-known/oauth-authorization-server` | RFC 8414 | Advertises the authorize, token, and registration endpoints |
| `POST /oauth/register` | RFC 7591 | Dynamic client registration — MCP clients arrive unknown and must self-register |
| `GET /oauth/authorize` | OAuth 2.1 + PKCE (RFC 7636) | The consent screen, rendered against the existing session |
| `POST /oauth/token` | OAuth 2.1 | Code exchange and refresh |

Plus `WWW-Authenticate` on a 401 extended with `resource_metadata=`, which is
the pointer that makes an unauthenticated client able to bootstrap itself
instead of simply failing.

Two things that are easy to skip and should not be:

- **Resource indicators (RFC 8707).** A token issued for North must not be
  replayable against another MCP server the same client talks to. Bind the
  audience.
- **The consent screen is a real screen.** It names the account, names the
  client, and says in plain words what the client will be able to read and
  change. A screen that only says "Approve?" is a screen nobody reads.

## What phase 1 got right, and must stay right

These were chosen so this phase is an addition rather than a rewrite. Do not
undo them:

| Decision | Why it matters here |
| --- | --- |
| `mcpserver.Authenticator` is an interface | An OAuth token verifier is a third implementation next to `StaticAuthenticator` and `connections.Service`, not a change to `authenticate` |
| Every auth failure is 401 with `WWW-Authenticate: Bearer realm="north-mcp"` | Phase 3 appends `resource_metadata=` to that header. A 403 would leave a client with nowhere to go |
| `/mcp` is public, mounted outside the CSRF and session group | The `.well-known` endpoints need the same treatment, and `cmd/web/main.go` already has the group to put them in |
| No token→user cache | Short-lived access tokens make a stale cache a correctness bug rather than a performance one |
| Rejections are byte-identical for unknown, revoked, and malformed | There is a test pinning this. A "helpful" message added during OAuth work would reintroduce enumeration |
| Throttling is per account, not per token | One person's runaway agent must not throttle everyone. OAuth multiplies tokens per user, so this only matters more |

## What phase 1 got wrong for this phase

`agent_connections` has no `scopes` and no `expires_at`. That was deliberate —
inventing a scope vocabulary before anything consumed it is guaranteed rework —
but it means phase 3 opens with a migration against a table holding live
credentials. That is a worse afternoon than adding two defaulted columns would
have been, and it is the one call from phase 1 worth regretting.

The migration is still small:

```sql
ALTER TABLE agent_connections
    ADD COLUMN scopes     text NOT NULL DEFAULT '',
    ADD COLUMN expires_at timestamptz;
```

Empty scopes keep meaning full access, which is every token issued by hand.
`NULL` expiry keeps meaning "does not expire", which is also every token issued
by hand. Both are what the existing rows already are, so the migration needs no
backfill and the existing query keeps working with one added predicate:

```sql
AND (expires_at IS NULL OR expires_at > now())
```

An OAuth grant then becomes a row in the same table with a different issuance
path and a `client_kind` of `oauth`, which keeps one revoke button for both
kinds of connection.

## Decisions to make before starting

These are the questions that will stall the work if they are answered halfway
through rather than at the beginning.

**Do scopes ship with OAuth, or after it?** Read and write as one credential is
the current state and is honestly a bit much for a token pasted into a third
party. The minimum worth having is two: read-only, and read-write. Anything
finer needs a vocabulary, and a vocabulary needs a reason.

**How long is an access token good for?** Short enough that a leak expires,
long enough that refresh is not constant. An hour is the usual answer. Refresh
tokens then need storage, which is what `internal/shared/secret` already exists
to seal — the same sealer BYOK uses, with the same user-id binding.

**Does dynamic client registration stay open?** RFC 7591 with no
authentication means anyone can register a client. That is the intended design
for MCP, and it is still a public write endpoint that needs rate limiting and a
bounded row count.

**What happens to the pasted-token path?** Keeping both is the honest answer —
OAuth needs a browser, and headless deployments do not have one. But then the
settings page has two ways to connect and has to explain when each applies,
which is a design problem rather than an engineering one.

## What this does not fix

Worth writing down so the phase is not oversold:

- Somebody who does not use an MCP client at all is unaffected. Nothing makes
  them a user of this feature.
- A client whose configuration is a GUI dialog rather than a file still needs
  the user to find the dialog.
- The token still authorises full access to one account's coaching data until
  scopes exist.

## Recommendation

Not yet, and for the same reason `byok-plan.md` gave for BYOK: this is
supply-side work, and the measurement that matters is the strangers count.
Phase 1 is enough for anyone technical enough to be running Claude Code or
Codex in the first place, which is who has asked so far.

Build this when a non-technical person has tried to connect an agent and
failed. That is a real signal, it is cheap to wait for, and it will also tell
you which client they were using — which decides more of the design than
anything in this document.

---

## The four decisions, answered

Written 2026-09-07, when the work shipped. The questions are this document's
own, from "Decisions to make before starting".

| Decision | Answer | Why |
| --- | --- | --- |
| **Do scopes ship with OAuth, or after it?** | **With it. Two:** `north:read` and `north:read_write`, defaulting to read-write. | The machinery already existed — `Registry.IsReadOnly` was consumed for the `ReadOnlyHint` annotation — so enforcement was a filter at registration rather than new logic. And retrofitting later would have meant a *second* migration against live credentials, which is exactly the phase-1 regret recorded above. Read-write is the default because every hand-issued token is that, and a connector that cannot log a check-in is not the product. Nothing finer: a vocabulary needs a reason. |
| **How long is an access token good for?** | **One hour.** Refresh: 30 days, single use, rotating, sliding. | The usual answer, and right. An hour bounds a leak. Sliding means an actively used connection never expires and an abandoned one dies in a month, which is also the natural sweep. |
| **Does dynamic client registration stay open?** | **Yes.** Bounded six ways. | Gating it would put a human step in the middle of the one-URL flow, which is the whole feature. The bounds: a per-IP rate limit; public clients only, so there is never a secret to leak; field bounds on everything a client names itself; a dedupe key, so a stable callback registers once; a sweep for registrations that never produced a grant; and a hard ceiling that fails loudly rather than evicting silently. |
| **What happens to the pasted-token path?** | **Kept, and demoted.** | Headless machines have no browser, and `cmd/mcp-server`'s tailnet deployment depends on it. This document calls the two-ways-to-connect problem "a design problem rather than an engineering one", and the design answer was to stop presenting them as peers: the settings page leads with the URL and puts manual issuance behind "No browser on that machine?". |

## What this document got right, and one thing it got wrong

Right, and load-bearing: every phase-1 invariant in "What phase 1 got right,
and must stay right" held. The byte-identical 401 survived — with one
correction, below. `/mcp` staying outside the CSRF group was the right place
for discovery and the token endpoint. No token→user cache meant short-lived
tokens needed no invalidation. Per-account throttling meant OAuth multiplying
tokens per user changed nothing.

Wrong, and happily so: it predicts "an OAuth token verifier is a third
implementation next to `StaticAuthenticator` and `connections.Service`". It is
not. Because grants live in `agent_connections` and their access tokens carry
the same `nk_` prefix, `connections.Service.Authenticate` authenticates them
with no change beyond one query predicate. What was actually missing was the
*scope*, so `Authenticate` became a wrapper over a new `AuthenticateScoped`.
The seam held better than its author expected.

Three things this document does not mention, which cost real time:

- **CORS.** Claude's web client performs discovery and the token exchange from
  its own page. Without permissive CORS on the machine endpoints it fails with
  nothing visible in the response. `MCP_ALLOWED_ORIGINS` is still empty, so
  the browser client is deliberately not supported yet — Claude Code, Claude
  Desktop and Codex send no `Origin` and work unchanged.
- **Clickjacking.** There is no CSP anywhere in this application, so every
  page is framable, and a framable consent screen is a textbook target. The
  consent routes set `X-Frame-Options` and `frame-ancestors 'none'`. **The
  broader absence is still a follow-up.**
- **The 401 invariant was never actually tested.** The existing test asserted
  only that `WWW-Authenticate` was non-empty. It now asserts byte equality
  across every failure mode.

## The scoreboard

House style follows `advanced-gamification.md`: a number, where it is read
from, and a threshold. Two thresholds per row here, because a demand test has
to be able to say *failed* as clearly as it says *worked*.

**Window: four weeks from the first public link.**

| Number | Read from | Worked | Failed |
|---|---|---|---|
| Strangers who reached consent | `mcp_authorize_started` where the distinct id has no prior `user_registered` | ≥ 20 | < 5 — nobody saw the URL. The problem is distribution, and no amount of OAuth work fixes it |
| Consent conversion | `mcp_consent_approved` ÷ `mcp_authorize_started` | ≥ 60% | < 30% — the screen is scaring people, or the form inside it is too long |
| **Accounts created inside consent** | `mcp_consent_approved{account_created:true}` | ≥ 8, and ≥ 40% of approvals | ~0 — everyone who connects already had an account. This was supply-side polish and the recommendation above was right |
| Share of all new accounts | `user_registered{via:oauth_consent}` vs every other `via` | ≥ 25% | < 5% — the acquisition claim is unsupported; `/signup` is still the front door |
| Tokens that get used | distinct connections with `mcp_tool_called` within 24h of `mcp_token_issued` | ≥ 80% | < 50% — the client stored a token and the tools are not discoverable or not useful |
| Second-day agent use | connections with `mcp_tool_called` on two distinct local days | ≥ 5 accounts | ≤ 1 — a novelty, not a habit |
| Return to the web app | `$pageview` on `/app/*` within 7 days, for `via:oauth_consent` | ≥ 30% | < 10% — the account is an API key with a login page attached; revisit the onboarding decision in `onboarding.SeedForAgent` |
| **Guardrail:** registration abuse | `mcp_client_registered` per day vs `mcp_authorize_started` per day | ratio under ~3:1 | > 200/day with no matching authorizes — open registration is being farmed; lower the ceiling and the rate limit |

Two rows are the verdict. **Accounts created inside consent** decides whether
overriding the recommendation above was justified. **Consent conversion**
decides whether the screen is any good. Everything else is diagnosis.

## Still outstanding

- **claude.ai as a client.** Needs `https://claude.ai` in
  `MCP_ALLOWED_ORIGINS`, which is a one-line values change and should be a
  deliberate one rather than an accident.
- **A general CSP.** The consent screen is covered; nothing else is.
- **Audit attribution.** An OAuth token's writes reach `toolaudit` with
  `SurfaceMCP`, but not *which* connection did it. `/app/settings/activity`
  could name the agent.
- **The six tools defined in `internal/mcpserver`** are not audited and do not
  emit `mcp_tool_called`; only the shared registry's are. Moving them into the
  registry is the follow-up that removes the split, as that file already says.
