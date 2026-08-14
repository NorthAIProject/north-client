# Phase 3 — OAuth for the MCP endpoint

Written 2026-08-14, after phases 1 and 2 shipped. This is a decision record and
a starting point, not a build order. Nothing here is implemented.

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
