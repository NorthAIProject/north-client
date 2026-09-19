# North API

The JSON API is served from `/api/v1` on the web application and may also be
exposed through the API hostname, for example:

```text
https://kheprios.com/api/v1
https://api.khepri.com/api/v1
```

The public contract is [`openapi.yaml`](openapi.yaml).

## Authentication

API requests use a North personal access token:

```http
Authorization: Bearer nk_...
``` 
For private views. Trials have a 7 day access to the app 

The API does not authenticate with the browser session cookie and is mounted
outside the SSR CSRF middleware. Tokens are resolved to their owning account;
clients never send a user ID to select an account.

## Adding an endpoint

1. Put API-specific request and response DTOs beside the feature API handler.
2. Reuse the feature service; do not put business rules in the HTTP handler.
3. Register the feature under `mountAPI` in `cmd/web/api.go`.
4. Keep paths relative to `/api/v1`; feature packages must not add `/v1`.
5. Use `internal/shared/httpx` for JSON decoding, encoding, and errors.
6. Add the path, schemas, authentication, and error responses to `openapi.yaml`.
7. Add route and handler tests before changing the contract.

## Conventions

- JSON fields use `camelCase`.
- UUIDs are encoded as strings.
- Timestamps use RFC 3339.
- Request bodies are bounded and reject unknown fields by default.
- Errors use the shared `{ "error": { "message": "...", "fields": {} } }` shape.
- Internal failures return a generic client message; implementation details do
  not cross the API boundary.
- API versions are path-based. Breaking changes require a new version.