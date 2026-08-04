#!/usr/bin/env bash
# Build the Tailwind CSS bundle into web/assets/css/output.css.
#
# Required before any `go build`/`go test`: the CSS is generated, not committed
# (see .gitignore), and web/assets embeds the css/ directory so a missing build
# fails at compile time rather than silently serving an unstyled app.
#
# Mirrors the Dockerfile Tailwind step so local, CI, and image builds stay aligned.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v tailwindcss >/dev/null 2>&1; then
  echo "tailwindcss not found on PATH" >&2
  exit 1
fi

# Ensure the module zip is in the cache. `go list -m -f '{{.Dir}}'` returns an
# empty Dir when the module is only in go.mod/go.sum and has never been
# downloaded — common in CI right after checkout — and a bare `test -d` then
# exits 1 with no message under set -e.
echo "resolving templui module…"
go mod download github.com/templui/templui

TEMPLUI_PATH="$(go list -m -f '{{.Dir}}' github.com/templui/templui)"
if [[ -z "$TEMPLUI_PATH" ]]; then
  echo "templui module Dir is empty after go mod download" >&2
  go list -m -json github.com/templui/templui >&2 || true
  exit 1
fi
if [[ ! -d "$TEMPLUI_PATH/components" ]]; then
  echo "templui components dir missing: $TEMPLUI_PATH/components" >&2
  ls -la "$TEMPLUI_PATH" >&2 || true
  exit 1
fi

printf '%s\n' \
  '@source "./**/*.templ";' \
  '@source "./**/*.js";' \
  "@source \"$TEMPLUI_PATH/components/**/*.templ\";" \
  "@source \"$TEMPLUI_PATH/components/**/*.js\";" \
  > ./web/assets/css/sources.generated.css

echo "running tailwindcss…"
tailwindcss -i ./web/assets/css/input.css -o ./web/assets/css/output.css --minify

if [[ ! -s ./web/assets/css/output.css ]]; then
  echo "tailwindcss produced an empty output.css" >&2
  exit 1
fi

echo "OK: wrote web/assets/css/output.css ($(wc -c < ./web/assets/css/output.css) bytes)"
