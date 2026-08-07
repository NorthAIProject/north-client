# Troubleshooting the Hermes ↔ North connection

Work from the symptom. Each of these fails as a timeout or a generic refusal,
so guessing is expensive.

## `/healthz` times out from a pod, works from your laptop

Two different causes, distinguished by whether the name resolves.

**DNS.** Check first, because it is the cheaper failure to rule out:

```bash
kubectl run -it --rm dnstest --image=busybox --restart=Never -- nslookup hermes-vps-2.<tailnet>.ts.net
```

No answer means CoreDNS has no stanza for the tailnet domain. Pods resolve
through CoreDNS, and the node's own `--accept-dns=false` means the node's
resolver is not shared with them. Add a server block for the tailnet domain
with a static `hosts` entry for the VPS.

Prefer a static entry over forwarding to `100.100.100.100`: on a single-node
cluster tailscaled answers only on the host, so the forward looks correct and
never resolves.

**Routing.** If the name resolves and the connection still hangs, the packets
are being dropped at the far end. flannel masquerades pod egress to the node's
public IP, and tailscaled discards tunnel packets whose source is not a tailnet
address. Confirm from the node:

```bash
tailscale ping hermes-vps-2       # works from the node
kubectl exec <pod> -- wget -qO- http://hermes-vps-2.<tailnet>.ts.net:8642/health   # hangs
```

That asymmetry is the signature. The fix is a SNAT rule rewriting pod-CIDR
traffic bound for the tailnet range to the node's tailnet address, applied on a
loop so it survives a tailscaled restart reassigning the interface.

## 401 on every MCP call, and the token is right

Check the header shape. It must be:

```
Authorization: Bearer <token>
```

Hermes's `--auth header` prompts for the whole header line, not just the value.
A registration holding only the token produces a 401 that looks exactly like a
wrong token.

If North logs `mcp request rejected` the request arrived and the token did not
match. If nothing appears in North's log at all, the request never got there —
that is the networking section above.

## North returns 500 with "MCP_USER_ID does not resolve to an account"

The configured UUID has no matching user. It is not a startup check, because
the database may not be reachable when the process starts. Confirm:

```sql
SELECT id, email FROM users WHERE id = '<MCP_USER_ID>';
```

## The coach answers, but ignores goals and check-ins

The MCP server builds its own coach with its own context sources, separate from
the web application's. A source added to one and not the other produces exactly
this: a coach that works but is less grounded through one door than the other.
Compare the `NewContextBuilder` call in `cmd/mcp-server/main.go` against the one
in `cmd/web/main.go`.

## North's coach fails only when a specific provider is at the head of the chain

Two known shapes:

**Form video analysis fails.** The OpenAI-dialect backends — OpenRouter, NVIDIA,
xAI, Hermes — have no file upload API. Analysis uses `AI_UPLOAD_PROVIDER`
(default `gemini`) rather than the chain, so it needs a Gemini key regardless of
what the coach is pointed at.

**Structured output comes back as prose.** Providers that do not support strict
`response_format: json_schema` are asked for the shape in the prompt instead,
which they can ignore. North retries once. If it fails consistently, that model
is a poor fit for structured work — put a provider that supports strict mode at
the head of the chain, or pick a different model for that backend.

## Verifying the whole path

```bash
# 1. North's MCP server is up
curl -s -o /dev/null -w '%{http_code}\n' http://<north-host>:8093/healthz     # 200

# 2. Authentication is enforced
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://<north-host>:8093/mcp # 401

# 3. The Hermes gateway is up and challenging
curl -s -o /dev/null -w '%{http_code}\n' http://hermes-vps-2.<tailnet>.ts.net:8642/health   # 200
curl -s -o /dev/null -w '%{http_code}\n' http://hermes-vps-2.<tailnet>.ts.net:8642/v1/models # 401

# 4. The tools are registered
npx @modelcontextprotocol/inspector --cli http://<north-host>:8093/mcp \
  --header "Authorization: Bearer $MCP_API_TOKEN" --method tools/list
```

Seven tools should be listed. Fewer means `Register` returned early; check
North's log for a service that failed to construct.
