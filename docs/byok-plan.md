# Bring-your-own-key, tiering, and who pays for inference

Written 2026-08-07, alongside the multi-provider work. This is a decision
record, not a build order — the recommendation at the bottom is to **not build
BYOK yet**, and the reasoning matters more than the schema.

> **Superseded 2026-08-14.** BYOK is built: `internal/aicreds`,
> `internal/shared/secret`, and the provider card on
> `/app/settings/connections`. The trigger this document named — "build BYOK
> when a user asks to plug their own key in" — was met, and the agent-connection
> work needed the same encryption primitive anyway, so the marginal cost of the
> outbound half dropped to a migration and a resolution path.
>
> The reasoning below still holds and is why the design came out as it did: the
> free tier is untouched, North still does not sponsor frontier inference, the
> registry stayed immutable, and a user's own credential is resolved per request
> in front of `ai.ChainSet` rather than becoming a second chain concept.
>
> What was **not** taken from here: the OpenRouter PKCE flow. Keys are pasted
> for every provider, deliberately — PKCE is one provider's mechanism, and
> shipping it first would have left the other four with nothing. See
> `docs/mcp-oauth-plan.md` for the OAuth work that is still outstanding, which
> is about the inbound MCP endpoint rather than this.

## The question

Should North sponsor xAI (Grok) keys for its users, or should each user bring
their own key for xAI and for a self-hosted Hermes gateway?

## Sponsoring frontier inference: no

Grok tokens cost real money per message. Sponsoring them means an unbounded
per-user cost with no revenue against it, and a single motivated user can drain
a month's budget in an afternoon. It also turns North into an inference
reseller, which carries a thin margin and drags in abuse handling, spend caps,
and payment operations — three businesses that have nothing to do with coaching.

The distinction that decides it is **marginal cost, not absolute cost**:

| Tier | Backend | Cost to North |
| --- | --- | --- |
| Free | NVIDIA free models | zero marginal |
| Free, overflow | the self-hosted Hermes gateway | fixed; already paid, capped by the box |
| Paid | the user's own key (BYOK) | zero marginal |

Fixed cost is fine — the VPS costs the same whether it serves one person or
fifty, and when it saturates the symptom is slowness, not a bill. Per-token cost
per stranger is not fine, because it scales with usage by people who are not
paying.

This is what `AI_PROVIDER_CHAIN_FREE=nvidia,hermes` already implements. Free
users cost nothing, and the configuration is identical at three users and three
hundred.

**Charge for the product, not for the tokens.**

## OAuth versus API keys

OAuth is the better mechanism where it exists — revocable, scoped, and North
never stores a credential. It exists for exactly one of the providers in play:

| Provider | Mechanism | Notes |
| --- | --- | --- |
| OpenRouter | **OAuth (PKCE)** | Use it. The user authorises; North holds a token it can drop. |
| xAI | API key | No OAuth offered. |
| NVIDIA | API key | No OAuth offered. |
| Hermes gateway | static `API_SERVER_KEY` | Self-hosted; a bearer is what it supports. |

So the answer is both: OAuth for OpenRouter, keys for the rest. What should
*not* happen is a general-purpose OAuth abstraction over providers that do not
offer OAuth — that is a framework written for a single implementer, and the
three key-based providers would each need a bypass through it anyway.

## What BYOK would cost to build

The load-bearing detail is that North's provider registry is built once at
startup and is deliberately lock-free:

> It is built once during startup and then only read, so it needs no locking.
> Registering after the server is serving would be a bug, not a feature.
> — `internal/ai/registry.go`

Per-user credentials break that assumption. The fix is **not** to make the
registry mutable and start locking it. `openaicompat.New` builds a struct around
a shared `http.Client`; construction is close to free, so a per-user client
should be constructed per request and the registry left immutable, serving
platform providers only.

Sketch:

- `user_ai_credentials(user_id, provider, ciphertext, base_url, model)`
- AES-GCM, key from the environment or a KMS. Never logged. Never rendered back
  to the user — a write-only field showing the last four characters, because a
  settings page that can display a key is a settings page that can leak one.
- Resolution order per request: the user's own credential, else the platform
  free provider.
- The existing `ai.ChainSet` already carries the per-tier ordering; BYOK adds a
  credential lookup in front of it, not a second chain concept.

Realistically a migration, a repository, crypto helpers, a settings page, and
the per-request resolution path. A day or two of focused work.

## Recommendation: not yet

BYOK is a monetization feature, and North has no one to monetize. The free
NVIDIA tier already lets a stranger use the product end to end at zero marginal
cost — which is the only thing needed to find out whether anyone wants it.

Build BYOK when a user asks to plug their own key in. Until then it is
supply-side work that cannot be validated, and the free tier is doing its job.

## Marketing, briefly

The free tier *is* the marketing: an AI coach that works with no credit card,
because free models cost nothing to serve. That is the offer.

Beyond that, North does not have a marketing-strategy problem — a strategy
already exists in `mvp-marketing.md` (2026-07-25): a four-week sprint to ten
strangers, channels ranked as X build-in-public, then Reddit, then direct
messages, with HN and Product Hunt as later one-shots. It is unexecuted, not
wrong.

The measurement that matters is the strangers count, and the next target is one
public link and one message sent — not one more feature.
