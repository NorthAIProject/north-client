#!/usr/bin/env bash
# Walk the OAuth dance against a running Khepri, the way a client does.
#
# Every step a real MCP client performs, in order, so a broken discovery
# document or a mismatched resource shows up here rather than as an unhelpful
# error inside somebody's editor. It exists because this gets re-run on every
# change to internal/mcpauth, and retyping six curls is how a step gets skipped.
#
# Usage:
#   scripts/oauth-dance.sh [base-url]
#
# The one step it cannot do is Approve — that is a person clicking a button in
# a browser, which is the entire point of the feature. It prints the authorize
# URL, waits for the code, and finishes the exchange.
set -euo pipefail

BASE="${1:-http://localhost:8090}"
BASE="${BASE%/}"
REDIRECT="http://127.0.0.1:41999/cb"

need() { command -v "$1" >/dev/null || { echo "need $1" >&2; exit 1; }; }
need curl
need python3

say() { printf '\n\033[1m%s\033[0m\n' "$1"; }
fail() { printf '\033[31m%s\033[0m\n' "$1" >&2; exit 1; }

# --- 1. protected resource metadata ----------------------------------------
# RFC 9728. A client resolving <base>/mcp asks for the /mcp-suffixed path; the
# bare one is served too because several clients still ask for it.
say "1. GET /.well-known/oauth-protected-resource/mcp"
PR=$(curl -fsS "$BASE/.well-known/oauth-protected-resource/mcp") || fail "no protected-resource document"
echo "$PR" | python3 -m json.tool

RESOURCE=$(echo "$PR" | python3 -c 'import sys,json;print(json.load(sys.stdin)["resource"])')
[ "$RESOURCE" = "$BASE/mcp" ] || fail "resource is $RESOURCE, want $BASE/mcp — BASE_URL and the URL you are calling disagree, which is the most common failure here"

# --- 2. authorization server metadata --------------------------------------
say "2. GET /.well-known/oauth-authorization-server"
AS=$(curl -fsS "$BASE/.well-known/oauth-authorization-server") || fail "no authorization-server document"
echo "$AS" | python3 -m json.tool

echo "$AS" | python3 -c '
import sys, json
d = json.load(sys.stdin)
methods = d["code_challenge_methods_supported"]
assert methods == ["S256"], f"code_challenge_methods_supported is {methods}, want only S256"
assert d["token_endpoint_auth_methods_supported"] == ["none"], "this server issues no client secrets"
' || fail "the authorization-server document advertises something it should not"

# --- 3. the 401 that starts it all -----------------------------------------
# An unauthenticated client reads resource_metadata off this header. Without
# it, nothing above is discoverable.
say "3. the 401 on /mcp points back at discovery"
CHALLENGE=$(curl -sS -o /dev/null -D - -X POST "$BASE/mcp" | tr -d '\r' | grep -i '^www-authenticate:' || true)
echo "${CHALLENGE:-<none>}"
case "$CHALLENGE" in
  *resource_metadata=*) ;;
  *) fail "the 401 carries no resource_metadata pointer" ;;
esac

# --- 4. dynamic client registration ----------------------------------------
say "4. POST /oauth/register"
REG=$(curl -fsS -X POST "$BASE/oauth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"client_name\":\"oauth-dance.sh\",\"redirect_uris\":[\"$REDIRECT\"],\"token_endpoint_auth_method\":\"none\"}") \
  || fail "registration refused"
echo "$REG" | python3 -m json.tool

CLIENT_ID=$(echo "$REG" | python3 -c 'import sys,json;print(json.load(sys.stdin)["client_id"])')
echo "$REG" | python3 -c '
import sys, json
d = json.load(sys.stdin)
assert "client_secret" not in d, "the server issued a client secret; every MCP client is public"
' || fail "unexpected client_secret in the registration response"

# --- 5. PKCE ---------------------------------------------------------------
say "5. PKCE (S256)"
read -r VERIFIER CHALLENGE_S256 <<<"$(python3 -c '
import base64, hashlib, secrets
v = base64.urlsafe_b64encode(secrets.token_bytes(48)).rstrip(b"=").decode()
c = base64.urlsafe_b64encode(hashlib.sha256(v.encode()).digest()).rstrip(b"=").decode()
print(v, c)
')"
echo "verifier:  ${VERIFIER:0:12}…"
echo "challenge: $CHALLENGE_S256"

STATE=$(python3 -c 'import base64,secrets;print(base64.urlsafe_b64encode(secrets.token_bytes(12)).rstrip(b"=").decode())')

AUTHORIZE="$BASE/oauth/authorize?response_type=code&client_id=$CLIENT_ID"
AUTHORIZE="$AUTHORIZE&redirect_uri=$(python3 -c "import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1],safe=''))" "$REDIRECT")"
AUTHORIZE="$AUTHORIZE&code_challenge=$CHALLENGE_S256&code_challenge_method=S256&state=$STATE"
AUTHORIZE="$AUTHORIZE&resource=$(python3 -c "import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1],safe=''))" "$BASE/mcp")"

# --- 6. the part a person does ---------------------------------------------
say "6. open this, approve it, and paste the code from the redirect"
echo "$AUTHORIZE"
echo
echo "The browser will land on $REDIRECT?code=…&state=… — copy the code."
printf 'code: '
read -r CODE
[ -n "$CODE" ] || fail "no code"

# --- 7. token exchange -----------------------------------------------------
say "7. POST /oauth/token"
TOKENS=$(curl -fsS -X POST "$BASE/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "grant_type=authorization_code" \
  --data-urlencode "code=$CODE" \
  --data-urlencode "client_id=$CLIENT_ID" \
  --data-urlencode "redirect_uri=$REDIRECT" \
  --data-urlencode "code_verifier=$VERIFIER" \
  --data-urlencode "resource=$BASE/mcp") || fail "the exchange was refused"

echo "$TOKENS" | python3 -c '
import sys, json
d = json.load(sys.stdin)
print(json.dumps({**d, "access_token": d["access_token"][:12] + "…", "refresh_token": d.get("refresh_token","")[:12] + "…"}, indent=2))
'
ACCESS=$(echo "$TOKENS" | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')
REFRESH=$(echo "$TOKENS" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("refresh_token",""))')

# --- 8. does the token actually work? --------------------------------------
say "8. tools/list with the issued token"
TOOLS=$(curl -fsS -X POST "$BASE/mcp" \
  -H "Authorization: Bearer $ACCESS" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2025-06-18' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}') || fail "the issued token was refused by /mcp"

echo "$TOOLS" | sed -n 's/^data: //p' | python3 -c '
import sys, json
raw = sys.stdin.read().strip() or "{}"
d = json.loads(raw)
tools = d.get("result", {}).get("tools", [])
print(f"{len(tools)} tools")
for t in tools[:5]:
    print(" ", t["name"])
if len(tools) > 5:
    print(f"  … and {len(tools)-5} more")
' 2>/dev/null || echo "$TOOLS" | head -c 400

# --- 9. refresh, and the replay that must fail -----------------------------
if [ -n "$REFRESH" ]; then
  say "9. POST /oauth/token (refresh_token)"
  REFRESHED=$(curl -fsS -X POST "$BASE/oauth/token" \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    --data-urlencode "grant_type=refresh_token" \
    --data-urlencode "refresh_token=$REFRESH" \
    --data-urlencode "client_id=$CLIENT_ID") || fail "the refresh was refused"
  echo "rotated"

  # Single use. Presenting the spent one again must be refused, and must retire
  # the grant — that is RFC 6749 section 10.5 and it is easy to regress.
  say "10. reusing the spent refresh token (must fail)"
  if curl -fsS -o /dev/null -X POST "$BASE/oauth/token" \
      -H 'Content-Type: application/x-www-form-urlencoded' \
      --data-urlencode "grant_type=refresh_token" \
      --data-urlencode "refresh_token=$REFRESH" \
      --data-urlencode "client_id=$CLIENT_ID" 2>/dev/null; then
    fail "a spent refresh token was accepted — reuse detection has regressed"
  fi
  echo "refused, as it must be"
fi

say "the dance completed"
