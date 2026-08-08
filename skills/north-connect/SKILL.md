---
name: north-connect
description: Use when the user wants to connect Hermes to North — registering North's MCP server so Hermes can read goals, log check-ins, and ask the coach, or pointing North's coach back at the Hermes gateway as an LLM backend. Covers both directions and the tailnet networking they depend on.
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

North runs an MCP server over Streamable HTTP. Register it:

```bash
hermes mcp add north --url http://<north-host>:8093/mcp --auth header
```

When prompted for the header, use `Authorization: Bearer <MCP_API_TOKEN>` —
the same value set in North's environment.

Confirm it is reachable before registering, since a failed registration looks
identical to a wrong token:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://<north-host>:8093/healthz   # expect 200
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://<north-host>:8093/mcp   # expect 401
```

A 200 on `/healthz` and a 401 on `/mcp` together mean the server is up and
authentication is working. Anything else is a networking problem, not a
credential one — see Troubleshooting.

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

- **Every call acts as one fixed account.** There is no user parameter and no
  way to act as anyone else.
- **Ask before writing.** `create_check_in`, `add_goal_update` and
  `calculate_macros` change the user's record. Confirm the details you are about to write, in their words.
- **Do not paraphrase a check-in into shape.** Mood and energy are the user's
  own read on their day. Ask for the numbers rather than inferring them from
  tone.

## Direction 2 — North uses Hermes (LLM backend)

The Hermes gateway speaks the OpenAI chat dialect, so North treats it as an
ordinary provider. In North's environment:

```bash
HERMES_BASE_URL=http://hermes-vps-2.<tailnet>.ts.net:8642/v1
HERMES_API_KEY=<the gateway's API_SERVER_KEY>
HERMES_MODEL=hermes-3
```

Then put it in the chain. North tries providers in order and moves to the next
when one refuses — out of credit, rate limited, overloaded, or a rejected key:

```bash
AI_PROVIDER_CHAIN=openrouter,hermes,nvidia
```

Hermes sitting behind a paid provider is the useful arrangement: it costs
nothing per token, so it catches the day the paid balance runs out.

Confirm the model name rather than trusting `hermes-3`:

```bash
curl -s -H "Authorization: Bearer $HERMES_API_KEY" \
  http://hermes-vps-2.<tailnet>.ts.net:8642/v1/models
```

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

- **Expose port 8093 publicly.** One static token, one account, no scopes. It
  belongs on the tailnet.
- **Put the token in a URL.** It is a header. URLs end up in logs.
- **Register North twice under different names.** Duplicate tool names across
  MCP servers make tool selection unpredictable.
