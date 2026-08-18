# Environment variables for a hosted North

> What the platform repo has to put on the k3s Deployment so a public
> North (for example `https://www.duxos.ai`) actually works.
>
> Source of truth is `internal/config/config.go`. This file is the operator
> view of the same list: what must be set, what can stay at its default, and
> what must **not** be copied from a laptop `.env`.
>
> Written 2026-08-18. If a variable is not in this file, North does not read
> it.

Local development stays in `.env` / `.env.example`. Do not paste that file
into the cluster. The defaults are for `localhost`.

---

## Which process this is for

The published image (`Dockerfile`) runs **`cmd/web` only**. That process
serves the site, `POST /mcp`, `POST /ingest/health`, and the Telegram
webhook.

Two other binaries share the same `config.Load()` but are **not** in the
image today:

| Binary | Role | On a public k3s cluster |
|---|---|---|
| `cmd/web` | HTTP + per-user MCP at `/mcp` | Yes — this is the app |
| `cmd/worker` | Background jobs (document index, video analysis, reports, Strava, memory extraction) | Same secrets as web, when that Deployment exists. Jobs otherwise sit in `jobs` forever |
| `cmd/mcp-server` | One static token, one account, listen `:8093` | **No.** Private / tailnet only. Public MCP is `/mcp` on web |

Give web and worker the same `DATABASE_URL`, `ENCRYPTION_KEY`, storage, and
AI keys. If both listen for metrics on one host, give them different
`METRICS_LISTEN_ADDR` values.

---

## What production needs

A laptop can omit Google, Strava, Telegram, PostHog, and embeddings — the
binary still starts. **A production deploy cannot.** Those features are
part of the product. Leaving them empty ships a site that boots and is
missing sign-in, fitness, messaging, analytics, and semantic search.

The process will not crash to tell you. Check the platform Secret against
this list before the first public cutover.

| Variable | Set it to | Why |
|---|---|---|
| `GO_ENV` | `production` | Cookie flags, CSRF, JSON logs, embedded assets. The image already sets this; keep it explicit in the platform Secret/ConfigMap so a override cannot slip back to `development` |
| `BASE_URL` | `https://www.duxos.ai` | Public origin with **no** `:8090`. Builds MCP snippets, OAuth callbacks, WebAuthn RP ID, Strava redirect, Telegram webhook URL, password-reset links |
| `DATABASE_URL` | cluster Postgres DSN | **Required to boot.** Needs pgvector (the Compose image is `pgvector/pgvector:pg17`) |
| `ENCRYPTION_KEY` | `openssl rand -base64 32` | Seals BYOK provider keys and Strava tokens. Unset still boots, but BYOK is unavailable and Strava tokens are stored in plaintext. Set-but-malformed **fails the boot** |
| `STORAGE_ENDPOINT` | the real S3 / R2 / MinIO URL | Default is `http://localhost:9000` |
| `STORAGE_BUCKET` | the media bucket | Default `north-media` is fine if that bucket exists |
| `STORAGE_ACCESS_KEY` | bucket access key | Empty default: the client is built, then every upload fails |
| `STORAGE_SECRET_KEY` | bucket secret | Same |
| `STORAGE_USE_PATH_STYLE` | `false` for S3 and R2; `true` for MinIO | Default is `true` (MinIO). Wrong value looks like a credentials bug |
| `STORAGE_REGION` | bucket region, e.g. `auto` on R2 | Default `us-east-1` |
| At least one provider key that appears in `AI_PROVIDER_CHAIN` | see [AI](#ai) | Otherwise the registry has only `fake`, and the coach replies with the fake string |
| `GOOGLE_CLIENT_ID` + `GOOGLE_CLIENT_SECRET` | Google Cloud OAuth client | "Continue with Google". Empty hides the button |
| `STRAVA_CLIENT_ID` + `STRAVA_CLIENT_SECRET` | Strava API application | Fitness connect. Empty makes the integration report itself unavailable |
| `TELEGRAM_BOT_TOKEN` + `TELEGRAM_BOT_USERNAME` + `TELEGRAM_WEBHOOK_SECRET` | @BotFather token, bot name, `openssl rand -hex 32` | Messaging gateway. Empty builds no adapter. Production uses webhook mode, not polling |
| `POSTHOG_API_KEY` | PostHog project key | Coach LLM observability. Empty in production is a silent no-op dashboard |
| `EMBEDDING_PROVIDER` + `EMBEDDING_MODEL` | `nvidia` + `nvidia/nv-embedqa-e5-v5` | Semantic document retrieval. Empty leaves full-text only. NVIDIA NIM is the backend that actually serves `/embeddings` |

`PORT` can stay at `8090`. TLS and the hostname belong on the Ingress. The
pod listens HTTP; `BASE_URL` is still `https://…`.

### `BASE_URL` is the one from the settings conversation

Settings renders MCP setup as `BASE_URL + "/mcp"`. Leave
`BASE_URL=http://localhost:8090` on the cluster and every user is handed:

```json
"url": "http://localhost:8090/mcp"
```

Set it to the origin people type in a browser:

```bash
BASE_URL=https://www.duxos.ai
```

No trailing slash. Scheme `https`. Host exactly as served (apex vs `www` is
a real difference for OAuth and WebAuthn). No port if 443 is the public one.

That same value is the Google redirect `{BASE_URL}/auth/google/callback`,
the Strava redirect `{BASE_URL}/app/fitness/strava/callback`, and the
Telegram webhook `{BASE_URL}/webhooks/telegram`.

---

## Suggested platform set (copy into the platform repo)

Non-secret ConfigMap:

```bash
GO_ENV=production
PORT=8090
BASE_URL=https://www.duxos.ai
LOG_LEVEL=info

# Replace with whatever the cluster actually runs.
AI_PROVIDER_CHAIN=openrouter,hermes,nvidia
AI_PROVIDER_CHAIN_FREE=nvidia,hermes
AI_UPLOAD_PROVIDER=gemini

OPENROUTER_SITE_URL=https://www.duxos.ai
OPENROUTER_SITE_NAME=North

EMBEDDING_PROVIDER=nvidia
EMBEDDING_MODEL=nvidia/nv-embedqa-e5-v5
EMBEDDING_DIMENSIONS=1024

WEBAUTHN_RP_DISPLAY_NAME=North
TELEGRAM_BOT_USERNAME=<bot username, no @ needed>

STORAGE_ENDPOINT=https://<r2-or-s3-endpoint>
STORAGE_REGION=auto
STORAGE_BUCKET=north-media
STORAGE_USE_PATH_STYLE=false

POSTHOG_HOST=https://us.i.posthog.com

# Empty is correct: real MCP clients send no Origin. A filled list is for
# a browser you have deliberately allowed.
MCP_ALLOWED_ORIGINS=

# Loopback inside the pod. Scrape over the tailnet or a NetworkPolicy,
# do not put this on the Ingress.
METRICS_LISTEN_ADDR=127.0.0.1:9090
```

Secret:

```bash
DATABASE_URL=postgres://north:<password>@<host>:5432/north?sslmode=require

ENCRYPTION_KEY=<openssl rand -base64 32>

STORAGE_ACCESS_KEY=<key>
STORAGE_SECRET_KEY=<secret>

# At least one of these, matching AI_PROVIDER_CHAIN. NVIDIA is also
# the embedding backend, so NVIDIA_API_KEY is required for semantic search.
OPENROUTER_API_KEY=
NVIDIA_API_KEY=
GEMINI_API_KEY=
XAI_API_KEY=
HERMES_API_KEY=
HERMES_BASE_URL=                    # required for hermes; there is no public default

GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=

STRAVA_CLIENT_ID=
STRAVA_CLIENT_SECRET=

TELEGRAM_BOT_TOKEN=
TELEGRAM_WEBHOOK_SECRET=            # openssl rand -hex 32; selects webhook mode

POSTHOG_API_KEY=
```

---

## Required to boot

`config.Load()` refuses to start unless these are valid.

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | none | Only variable that is strictly required **to boot**. `sslmode=require` on a real Postgres; `disable` is a laptop value |
| `ENCRYPTION_KEY` | unset | Unset is allowed by the loader. A value that is not 32 bytes of base64 **is not**. Production must set it |
| `AI_PROVIDER_CHAIN` | `gemini` via legacy `AI_PROVIDER` | The named providers must exist (`gemini`, `openrouter`, `nvidia`, `xai`, `hermes`, `fake`). At least one named provider must have credentials, or `providers.Build` fails |
| `EMBEDDING_PROVIDER` + `EMBEDDING_MODEL` | unset | Either both set or both empty. Dimensions must be `1024`. Production must set both |
| `PORT`, quota ints, `SESSION_LIFETIME`, `STORAGE_USE_PATH_STYLE` | see below | A non-empty value that does not parse fails the boot |

---

## Application

| Variable | Default | Hosted |
|---|---|---|
| `GO_ENV` | `development` | `production` |
| `PORT` | `8090` | Leave it. Service/Ingress map 443 → 8090 |
| `BASE_URL` | `http://localhost:8090` | Public `https://` origin, no trailing slash, no app port |
| `LOG_LEVEL` | `info` | `info` or `warn`. `debug` is noisy in production JSON logs |
| `SESSION_LIFETIME` | `720h` | Duration string (`720h`, `168h`). Sessions live in Postgres; there is no `SESSION_SECRET` |

Password-reset email is still `auth.LogMailer`. There is no SMTP variable.
Reset links appear in pod logs until a real mailer is wired.

---

## Object storage

Same shape for MinIO, S3, and Cloudflare R2. The process builds the client
at boot; it does not create the bucket.

| Variable | Default | Hosted |
|---|---|---|
| `STORAGE_ENDPOINT` | `http://localhost:9000` | Provider endpoint (`https://<account>.r2.cloudflarestorage.com`, or the cluster MinIO Service DNS) |
| `STORAGE_REGION` | `us-east-1` | Whatever the bucket uses. R2 commonly `auto` |
| `STORAGE_BUCKET` | `north-media` | Must already exist |
| `STORAGE_ACCESS_KEY` | empty | Required for uploads to work |
| `STORAGE_SECRET_KEY` | empty | Required for uploads to work |
| `STORAGE_USE_PATH_STYLE` | `true` | `true` for MinIO, `false` for S3 and R2 |

---

## AI

Providers are tried in order. A name whose key is missing is skipped at
boot (and logged), not a crash. `fake` is always registered. A production
chain that falls through to `fake` looks like a working coach and is not
one.

| Variable | Default | Hosted |
|---|---|---|
| `AI_PROVIDER_CHAIN` | `gemini` if unset | Paid / preferred first, then fallbacks. Example: `openrouter,hermes,nvidia` |
| `AI_PROVIDER_CHAIN_FREE` | same as the main chain | Cheap / self-hosted. Example: `nvidia,hermes` |
| `AI_PROVIDER` | `gemini` | Legacy single-provider form. Ignored when `AI_PROVIDER_CHAIN` is set |
| `AI_MODEL` | empty | Leave empty so each provider uses its own model |
| `AI_FAST_MODEL` | empty | Same. Used for cheaper side work (memory extraction) |
| `AI_UPLOAD_PROVIDER` | `gemini` | Form video analysis. Needs a provider with a file-upload API; the OpenAI-dialect backends do not have one |

Per-provider keys. A provider is on when its key (and, for Hermes, its
base URL) is set:

| Variable | Default | Notes |
|---|---|---|
| `GEMINI_API_KEY` | empty | |
| `GEMINI_MODEL` | `gemini-2.5-pro` | |
| `OPENROUTER_API_KEY` | empty | |
| `OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | Leave it |
| `OPENROUTER_MODEL` | `anthropic/claude-sonnet-4.5` | |
| `OPENROUTER_SITE_URL` | empty | Set to `BASE_URL` so OpenRouter ranking headers are right |
| `OPENROUTER_SITE_NAME` | `North` | |
| `NVIDIA_API_KEY` | empty | Free-tier head of `AI_PROVIDER_CHAIN_FREE` |
| `NVIDIA_BASE_URL` | `https://integrate.api.nvidia.com/v1` | Leave it |
| `NVIDIA_MODEL` | `meta/llama-3.3-70b-instruct` | |
| `XAI_API_KEY` | empty | |
| `XAI_BASE_URL` | `https://api.x.ai/v1` | Leave it |
| `XAI_MODEL` | `grok-4.5` | |
| `HERMES_API_KEY` | empty | The gateway's `API_SERVER_KEY`, not a North `nk_` token |
| `HERMES_BASE_URL` | empty | **No default.** Example `http://hermes-vps-2.<tailnet>.ts.net:8642/v1` |
| `HERMES_MODEL` | `hermes-3` | Confirm against `GET /v1/models` |

Hermes from inside k3s also needs tailnet DNS and SNAT on the node. Missing
either looks like a timeout, not a 401. See `skills/north-connect/SKILL.md`.

Users can still point **their own** Hermes gateway from Settings. The
process-wide `HERMES_*` values are only the shared fallback.

### Semantic retrieval

Required in production. Off unless both provider and model are set; the
loader accepts the empty pair so a laptop without NVIDIA still boots, but
a hosted North without embeddings is full-text only.

| Variable | Default | Production |
|---|---|---|
| `EMBEDDING_PROVIDER` | empty | `nvidia` — an OpenAI-dialect backend that serves `/embeddings`. NVIDIA NIM does; OpenRouter and xAI do not |
| `EMBEDDING_MODEL` | empty | `nvidia/nv-embedqa-e5-v5`. Must be set together with the provider |
| `EMBEDDING_DIMENSIONS` | `1024` | Leave at `1024` — that is the column width. Another width needs a migration |

`NVIDIA_API_KEY` has to be present for this pair to actually embed. Setting
the two names and leaving the key empty fails the embedder at boot.

---

## Encryption at rest

| Variable | Default | Hosted |
|---|---|---|
| `ENCRYPTION_KEY` | unset | Set it. `openssl rand -base64 32` |

Format: bare base64 (id `1`) or `id:base64`. Rotation puts the new key
first:

```bash
ENCRYPTION_KEY=2:<new>,1:<old>
```

Web and worker must share the same key material. A row sealed by one
process and unread by the other is a sync that fails on decrypt.

---

## Sign-in and passkeys

Required in production. Empty credentials hide "Continue with Google";
the binary still starts.

| Variable | Default | Production |
|---|---|---|
| `GOOGLE_CLIENT_ID` | empty | Google Cloud OAuth 2.0 client ID |
| `GOOGLE_CLIENT_SECRET` | empty | Must travel with the id |
| `WEBAUTHN_RP_ID` | host of `BASE_URL` | Hostname only (`www.duxos.ai`), never a URL. Leave empty unless it should differ from `BASE_URL` |
| `WEBAUTHN_RP_DISPLAY_NAME` | `North` | What the passkey prompt shows |

Google Cloud authorized redirect: `{BASE_URL}/auth/google/callback`.

---

## Fitness (Strava)

Required in production. Empty credentials make the integration report
itself unavailable; the rest of the app still starts.

| Variable | Default | Production |
|---|---|---|
| `STRAVA_CLIENT_ID` | empty | Strava API application ID |
| `STRAVA_CLIENT_SECRET` | empty | Must travel with the id |

Strava authorized callback: `{BASE_URL}/app/fitness/strava/callback`.
Tokens are sealed with `ENCRYPTION_KEY`.

---

## Telegram

Required in production. Empty `TELEGRAM_BOT_TOKEN` builds no adapter and
mounts no route. Setup walkthrough is in the README.

On a public host use **webhook** mode, not polling. Polling does not scale
past one replica. Setting `TELEGRAM_WEBHOOK_SECRET` is what selects
webhook mode.

| Variable | Default | Production |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | empty | From @BotFather |
| `TELEGRAM_BOT_USERNAME` | empty | Shown in Settings so people can find the bot. With or without `@` |
| `TELEGRAM_WEBHOOK_SECRET` | empty | `openssl rand -hex 32`. Presence selects webhook mode at `{BASE_URL}/webhooks/telegram` |

Register once after deploy:

```bash
curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
  -d url="$BASE_URL/webhooks/telegram" \
  -d secret_token="$TELEGRAM_WEBHOOK_SECRET"
```

---

## MCP (the public one)

Users create their own bearer tokens in Settings → Agent connections.
Those tokens are **not** environment variables. The snippet they copy uses
`BASE_URL`.

| Variable | Default | Hosted |
|---|---|---|
| `MCP_ALLOWED_ORIGINS` | empty | Leave empty. Empty rejects every browser `Origin`, which is correct |
| `MCP_REQUESTS_PER_MINUTE` | package default (120) | Per account, in memory, per replica |

### Do not put these on `cmd/web`

| Variable | Belongs to |
|---|---|
| `MCP_LISTEN_ADDR` | `cmd/mcp-server` only (default `127.0.0.1:8093`) |
| `MCP_API_TOKEN` | `cmd/mcp-server` only — one static token, one account |
| `MCP_USER_ID` | `cmd/mcp-server` only |

Exposing `:8093` on the Ingress is an account takeover waiting for a leaked
env. See `docs/gateways.md` and `skills/north-connect/SKILL.md`.

---

## Observability

| Variable | Default | Hosted |
|---|---|---|
| `POSTHOG_API_KEY` | empty | **Required in production and in development.** Development fails the boot without it. Production starts with a no-op client, which is how a live site ships a silent empty dashboard — set the key |
| `POSTHOG_HOST` | `https://us.i.posthog.com` | Leave unless the project is on EU cloud |
| `METRICS_LISTEN_ADDR` | `127.0.0.1:9090` | Prometheus scrape. Empty turns the listener **and** the counters off. Do not publish it on the Ingress; `/healthz` is the public probe |

---

## Quotas

Working defaults. Zero or a missing name means "use the default", never
"refuse everything". Per account per hour, stored in Postgres so replicas
agree.

| Variable | Default |
|---|---|
| `QUOTA_COACH_MESSAGES_PER_HOUR` | `30` |
| `QUOTA_DOCUMENT_UPLOADS_PER_HOUR` | `60` |
| `QUOTA_DOCUMENT_REINDEX_PER_HOUR` | `10` |
| `QUOTA_REPORT_GENERATIONS_PER_HOUR` | `10` |
| `QUOTA_MEDIA_ANALYSES_PER_HOUR` | `20` |
| `QUOTA_ACCOUNT_EXPORTS_PER_HOUR` | `3` |

---

## Do not set on the cluster

| Variable | Why |
|---|---|
| `TEST_DATABASE_URL` | Test suite only |
| `EVAL_PROVIDER`, `EVAL_MODEL` | `task test:live` only |
| Laptop `STORAGE_*` / `DATABASE_URL` pointing at `localhost` | The pod's localhost is the pod |
| `MCP_API_TOKEN` / `MCP_USER_ID` on the web Deployment | Wrong binary |
| `AI_PROVIDER_CHAIN=…,fake` as the production chain | Silent fake coach when the real key is missing |
| A comment on the same line as a value | Fine in Kubernetes YAML. Fatal in a godotenv file; mentioned because people copy `.env` into a Secret |

---

## After the env is in place

From outside the cluster:

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://www.duxos.ai/healthz
# 200

curl -s -o /dev/null -w '%{http_code}\n' -X POST https://www.duxos.ai/mcp
# 401 — up, and not CSRF. 403 means an Origin header leaked in.
```

Then open Settings → Agent connections. The snippet URL must be
`https://www.duxos.ai/mcp`, not `http://localhost:8090/mcp`.

Production is not done until these also work:

| Feature | What to see |
|---|---|
| Google | "Continue with Google" on the sign-in page; callback is `{BASE_URL}/auth/google/callback` |
| Strava | Connect Strava on the fitness / connections page; callback is `{BASE_URL}/app/fitness/strava/callback` |
| Telegram | Bot card in Settings with the configured username; webhook registered at `{BASE_URL}/webhooks/telegram` |
| PostHog | Generations appearing in the project after one coach turn |
| Embeddings | Boot log `embeddings=true`; a reindexed document is retrievable by meaning, not only by keyword |
