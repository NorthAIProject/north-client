---
name: north-connect
description: Use when the user wants to connect an agent to North — registering North's MCP server with Hermes, Claude Code, Codex, or any MCP client so it can read goals, log check-ins, and ask the coach, or pointing North's coach back at the Hermes gateway as an LLM backend. Covers both directions, per-user keys, and the tailnet networking the self-hosted path depends on.
---

# Connecting Hermes and North

North is an AI operating system for personal growth: goals, daily check-ins,
training, and a coach grounded in all of it. Hermes and North connect in two
independent directions. Work out which one the user wants before touching
anything — they solve different problems and neither implies the other.

| Direction | What it gives you | Set up |
| --- | --- | --- |
| **Hermes → North** | Hermes can read the user's goals and check-ins, log new ones, and ask their coach | Register North's MCP server with Hermes |
| **North → Hermes** | North's coach runs on the self-hosted Hermes gateway instead of a paid API | Set `HERMES_BASE_URL` and `HERMES_API_KEY` in North |

## Direction 1 — Hermes uses North (MCP)

North serves the same MCP tools from two places. Work out which one is in front
of you before touching anything, because the credential differs.

| | Web app — `https://<north-host>/mcp` | Standalone — `http://<north-host>:8093/mcp` |
| --- | --- | --- |
| Credential | A key the user creates in Settings | `MCP_API_TOKEN` from the environment |
| Acts as | Whoever created the key | The one account in `MCP_USER_ID` |
| Revoking | A button on the settings page | Editing the environment and redeploying |
| Where it belongs | Public | The tailnet, and nowhere else |

**Prefer the web app.** Its keys are per person, revocable without a restart,
and it is the only one that works when more than one person uses North. The
standalone binary (`cmd/mcp-server`) exists for a private single-user
deployment and is not going to grow per-user credentials.

### Getting a key (web app)

The user makes it themselves — you cannot, and neither can an operator:

1. Settings → Agent connections → **Manage connections**.
2. Name it after the machine it will live on, choose the client, create.
3. The key is shown **once**. It starts `nk_`. If it is lost, revoke that
   connection and make another; nothing can recover it.

That page also offers a ready-made setup prompt, which is worth using instead of
this section when the user would rather paste one block than follow steps.

### Registering it

```bash
hermes mcp add north --url https://<north-host>/mcp --auth header
```

When prompted for the header, use `Authorization: Bearer nk_…` — or
`Authorization: Bearer <MCP_API_TOKEN>` if this is the standalone server.

Confirm the endpoint is reachable before registering, since a failed
registration looks identical to a wrong key:

```bash
# Web app
curl -s -o /dev/null -w '%{http_code}\n' https://<north-host>/healthz          # expect 200
curl -s -o /dev/null -w '%{http_code}\n' -X POST https://<north-host>/mcp      # expect 401

# Standalone
curl -s -o /dev/null -w '%{http_code}\n' http://<north-host>:8093/healthz      # expect 200
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://<north-host>:8093/mcp  # expect 401
```

A 200 on `/healthz` and a 401 on `/mcp` together mean the server is up and
authentication is working. A **403** means the request carried an `Origin`
header — the endpoint rejects browsers on purpose. Anything else is a
networking problem, not a credential one; see Troubleshooting.

### The tools

| Tool | Use it when |
| --- | --- |
| `search_goals` | The user mentions a goal, or you need to know what they are working towards. Set `active_only` unless they ask about finished ones. |
| `add_goal_update` | They report progress. The goal is named by **title**, not ID; an ambiguous title is rejected rather than guessed. |
| `create_check_in` | They describe how their day went. Requires `mood` and `energy`, both 1–5. Writing twice on the same day replaces that day's entry, so it is safe to correct. |
| `list_check_ins` | You need recent context — how the week has actually gone rather than how they remember it. |
| `search_knowledge` | You need durable facts about the user: injuries, preferences, constraints. Only confirmed memories are returned. |
| `get_fitness_summary` | They ask about training volume, or you need to know whether they have actually been moving. |
| `ask_coach` | The question deserves North's own coaching context. Prefer this over answering from your own reasoning when the topic is their training or goals — the coach sees data you do not. |
| `search_documents` | They refer to their own notes, a training log, or something a professional wrote for them. Returns passages with a citable `ref`, the document and heading it came from, and its line range. |
| `knowledge_status` | Before concluding the user has not told North something. It reports what has actually been read, what is still waiting, and what could not be parsed. |
| `search_exercises` | You need a movement from North's catalogue rather than one you remember. |
| `get_exercise` | You have a slug from `search_exercises` and need the full entry. |
| `search_ingredients` | You need nutrition figures per 100g for a specific food. |
| `todays_nutrition` | They ask what they have eaten today, or you need intake against their targets. |
| `list_goals` | A no-argument list of active goals. `search_goals` is the better tool when you have a term. |
| `calculate_macros` | **Writes.** They ask for macro targets. This saves the result as their current plan, so confirm before calling it. |

Every tool declares whether it writes. A client running in read-only mode can
trust that annotation: the four that change anything are `add_goal_update`,
`create_check_in`, `ask_coach`, and `calculate_macros`.

Results carry structured content as well as text, so read
`structuredContent` rather than parsing the text block.

### Citing what you use

`search_documents` returns a `ref` for every passage, like
`chunk:nor_chk_1a2b…`. When you use a passage, quote its ref. North records
which stored facts produced a reply, and a ref you invented rather than
received breaks that record in the one direction that matters — it makes a
guess look like evidence.

### Behaviour

- **Every call acts as the account the key belongs to.** There is no user
  parameter and no way to act as anyone else — the key decides, not the caller.
- **Ask before writing.** `create_check_in`, `add_goal_update` and
  `calculate_macros` change the user's record. Confirm the details you are about to write, in their words.
- **Do not paraphrase a check-in into shape.** Mood and energy are the user's
  own read on their day. Ask for the numbers rather than inferring them from
  tone.

## Direction 2 — North uses Hermes (LLM backend)

This is independent of Direction 1. Hermes as an MCP *client* talking to
North does not make North's coach use Hermes as a *model*. For that, North
must call **that user's** Hermes gateway.

Each North account points at its own instance. The person sets this in
**Settings → Agent connections → Model provider → Hermes (your gateway)**:
gateway URL (for example `http://hermes-vps-2.<tailnet>.ts.net:8642/v1`)
and the gateway's `API_SERVER_KEY`.

The process-wide `HERMES_*` environment variables are only a fallback for
operators who run one shared gateway for everyone who has not set their own.

The gateway speaks the OpenAI chat dialect. In North's `.env`:

```bash
HERMES_BASE_URL=http://hermes-vps-2.<tailnet>.ts.net:8642/v1
HERMES_API_KEY=<the gateway's API_SERVER_KEY>
HERMES_MODEL=hermes-3
```

`HERMES_API_KEY` is the VPS `API_SERVER_KEY`, not a chat-provider key and
not a North `nk_` MCP token.

Then put Hermes in the chain. North tries providers in order and moves to the
next when one refuses — out of credit, rate limited, overloaded, or a
rejected key:

```bash
# Laptop: Hermes first, a paid key if the gateway is down, fake last.
AI_PROVIDER_CHAIN=hermes,openrouter,fake

# Until the gateway key exists, do not put hermes at the head:
# AI_PROVIDER_CHAIN=openrouter,nvidia,fake

# Production: paid provider first, Hermes as the free fallback.
AI_PROVIDER_CHAIN=openrouter,hermes,nvidia
```

Confirm the model name rather than trusting `hermes-3`:

```bash
curl -s http://hermes-vps-2.<tailnet>.ts.net:8642/health
# expect {"status":"ok",...}

curl -s -H "Authorization: Bearer $HERMES_API_KEY" \
  http://hermes-vps-2.<tailnet>.ts.net:8642/v1/models
# expect 200 and a model list. 401 means the key is wrong.
```

A missing `HERMES_API_KEY` or `HERMES_BASE_URL` skips Hermes at boot rather
than failing the process — the next name in the chain answers instead.

**MCP up does not mean the coach is real.** Listing tools on `/mcp` never
calls an LLM. If the chain is `hermes,fake` and the Hermes key is empty,
the coach replies with the fake provider string. Put a working provider
after Hermes, or set the key.

### From a laptop already on the tailnet

No Kubernetes, no CoreDNS, no SNAT. The Mac (or any tailnet node) resolves
MagicDNS and dials the gateway the same way `curl` does:

1. `tailscale status` shows `hermes-vps-2` as active.
2. `curl -s http://hermes-vps-2.<tailnet>.ts.net:8642/health` returns 200.
3. Set the three `HERMES_*` variables and `AI_PROVIDER_CHAIN=hermes,fake`.
4. Restart `task dev`. The web/worker boot log's `ai_provider` should be
   `hermes`.

If health is 200 but `/v1/models` is 401, the URL is right and the key is
not. The key lives on the VPS as `API_SERVER_KEY` (Hermes gateway config),
not in `~/.hermes/.env` on the laptop.

## Networking

Both directions cross the tailnet. On a machine already joined to it, this
works with no further setup — resolve the MagicDNS name and connect.

From inside Kubernetes it needs three things, and missing any one of them
produces a timeout rather than a useful error:

1. The node joined to the tailnet (`tailscale up --accept-dns=false`).
2. A CoreDNS stanza resolving the tailnet domain, so pods can look up
   `*.ts.net` names.
3. A SNAT rule rewriting pod egress to the node's tailnet address — without it
   flannel masquerades to the node's **public** IP and tailscaled silently
   drops the packets.

See `references/troubleshooting.md` for the symptoms of each.

## Do not

- **Expose port 8093 publicly.** The standalone server is one static token, one
  account, no scopes, and no revoke button — a leaked environment variable is a
  silent account takeover fixable only by a redeploy. It belongs on the tailnet.
  The web app's `/mcp` is the one built to be reachable.
- **Put the key in a URL.** It is a header. URLs end up in access logs, in
  `Referer`, and in browser history.
- **Write the key into a file inside a git repository.** `.mcp.json` is
  committed far more often than people expect. Put it in an environment variable
  and reference that from the config; the settings page hands out both forms.
- **Echo the key back in full.** Not into a terminal, not into a reply, not into
  a file the user did not ask for.
- **Register North twice under different names.** Duplicate tool names across
  MCP servers make tool selection unpredictable.
