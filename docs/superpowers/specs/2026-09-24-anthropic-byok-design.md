# Anthropic as a bring-your-own-key provider

Date: 2026-09-24
Status: approved in conversation, awaiting spec review

## Goal

A person can paste an Anthropic API key in Settings → Connections and have
Claude answer their coach turns, with every Khepri capability working: tool
calls, approval cards, exercise animation on Telegram and iOS, check-ins.

Success is a Telegram "show me how to do a squat" answered by Claude through
Khepri's own tools: `get_exercise` runs in the coach loop, the reply records the
exercise, and the GIF goes out ahead of the text.

## Scope

In:

- A catalogue entry, "Anthropic (Claude)", in `internal/ai/providers/catalog.go`.
- A new `internal/ai/anthropic` package implementing `ai.Client` over the
  native Messages API with the official Go SDK
  (`github.com/anthropics/anthropic-sdk-go`).
- Key verification for Anthropic in `internal/aicreds`.
- Tests: unit tests against a fake HTTP server; one live smoke test gated on
  `ANTHROPIC_API_KEY`.

Out:

- Anthropic in the platform chain (`AI_PROVIDER_CHAIN`, an operator key).
- Server-side refusal fallbacks (`fallbacks` beta).
- Embeddings (Anthropic has none) and transcription.

## Decisions

**Official SDK, not raw HTTP or the OpenAI shim.** The SDK owns SSE parsing,
retries and typed errors. The catalogue already records why the OpenAI
compatibility endpoint is not used. One new dependency, justified under the
CLAUDE.md dependency questions: maintained by the vendor, and the alternative is
a hand-written SSE parser.

**Default model `claude-opus-5`.** Changeable per credential in Settings (the
existing model field), e.g. `claude-sonnet-5` for lower cost.

**Tools are called natively.** `CallsTools()` stays true (the client does not
implement `ai.ToolCaller`), so the coach runs its own tool loop and the MCP
bridge from #52/#54 never applies.

## Components

### `internal/ai/anthropic`

`New(Options) (*Client, error)` with `Options{APIKey, DefaultModel, BaseURL,
HTTPClient}`. `BaseURL` exists only so tests can point at an `httptest` server.

`Name()` returns `"anthropic"`.

**Request mapping** (`ai.Request` → `MessageNewParams`):

| Khepri | Anthropic |
|---|---|
| `System` | `System: []TextBlockParam{{Text, CacheControl: ephemeral}}` |
| `Message{Role: user, Parts}` | user message; text parts → text blocks, `InlineData` image parts → base64 image blocks |
| `Message{Role: model, Parts}` | assistant message with text blocks |
| `Message{ToolCalls}` | assistant message with `tool_use` blocks (id, name, input), preceded by any carried thinking blocks (see below) |
| `Message{ToolResults}` | one user message with every `tool_result` block (`is_error` set) |
| `Tools` | `ToolParam{Name, Description, InputSchema}` from `ai.Schema` |
| `ResponseSchema` | `output_config.format` JSON schema |
| `MaxTokens` | `MaxTokens`, defaulting to 16000 when zero |
| `Temperature` | not sent: sampling parameters return 400 on Opus 5 |

Consecutive messages of the same role are merged, since the API requires
alternation and Khepri's history can hold, for example, a stored user message
followed by a tool-result user turn.

**Chat** streams via `client.Messages.NewStreaming`, forwarding each text delta
as a `StreamChunk{Text}`. It accumulates the message; at the end it emits one
chunk with all `ToolCalls` (if any), then one with `Usage` and `Model`. The
channel is always closed; an error is the last chunk.

**Generate** makes one non-streaming call and returns `Response{Text, Usage,
Model, ToolCalls, FinishReason}`.

**UploadFile** returns a `File` with an empty URI, so callers inline the bytes.

**Stop reasons.** `refusal` becomes an error the runner fails over on (a
"temporarily unavailable" class), so a refused turn is answered by the chain
rather than posted empty. `max_tokens` is reported through `FinishReason` as
today's clients do.

**Errors.** SDK errors are mapped to Khepri's classes with `errors.As` into
`*anthropic.Error`: 401 → `ErrForbidden` (bad key, reported on the settings
page), 402/credit errors → `ErrPaymentRequired`, 429 and 5xx → unavailable
(fail over), 400 → a caller error that does not walk the chain. Error text never
includes the key.

**Usage.** `InputTokens` includes cache-read and cache-creation tokens so the
spend ledger sees the whole input; the client is wrapped by `ai.Metered` with
`byok: true` as other own keys are.

### Thinking across tool rounds

Opus 5 thinks by default. When a turn continues after tool results, the
assistant message holding the `tool_use` blocks must carry that response's
thinking blocks back unchanged.

- `ai.Message` gains `ProviderState json.RawMessage`, documented as opaque and
  owned by the client that produced it; every other client ignores it.
- `Chat` puts the response's thinking blocks, serialised, on the tool-call
  chunk (`StreamChunk.ProviderState`), and `ai.ToolCallMessage` carries it into
  the next request, so a turn's in-memory tool loop keeps its thinking.
- A tool-call message rebuilt from the database after an approval has no
  state. For that request only, the client sends `thinking: disabled` (allowed
  on Opus 5 at effort `high` or below, which is the default).

The exact replay requirement is confirmed against the live documentation as the
first implementation step; if thinking blocks turn out to be optional on replay,
`ProviderState` is dropped and this section with it.

### Catalogue and verification

```go
{
    Name: "anthropic", Label: "Anthropic (Claude)",
    BaseURL: "https://api.anthropic.com",
    DefaultModel: "claude-opus-5", KeyHint: "sk-ant-…",
    Note: "Claude on your own Anthropic account. Billed to that account.",
}
```

`providers.User` builds the Anthropic client for this entry, as it does Gemini
today. `aicreds` verification gains an Anthropic path: a `GET /v1/models` with
`x-api-key` and `anthropic-version`, where 401/403 means a bad key. The settings
page lists the entry from the catalogue with no template change.

## Testing

Unit tests in `internal/ai/anthropic`, against an `httptest.Server` replaying
recorded SSE and JSON bodies:

- a streamed text reply arrives as ordered text chunks, then usage and model;
- a tool call arrives as one `ToolCalls` chunk with id, name and decoded input;
- a follow-up request carries `tool_use` and `tool_result` blocks with matching
  ids, all results in one user message, and the carried thinking blocks;
- a rebuilt tool-call message without state sends thinking disabled;
- an image part becomes a base64 image block;
- same-role messages are merged;
- 401, 429, 529 and `refusal` map to the right error classes;
- the system prompt carries `cache_control`.

Catalogue test: the entry exists and builds a client. Verification test: 401 is
reported as a rejected key.

Live smoke test, skipped without `ANTHROPIC_API_KEY`: one tool round trip
against `claude-opus-5`.

## Risks

- **Cost to the user.** A turn carries about 85k input tokens of context; caching
  the system prompt is what keeps repeat turns affordable. The live test reports
  `cache_read_input_tokens` so this is measured, not assumed.
- **SDK drift.** Type names come from the SDK's docs and compiler, not memory.
