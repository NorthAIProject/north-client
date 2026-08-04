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

TEMPLUI_PATH="$(go list -mod=mod -m -f '{{.Dir}}' github.com/templui/templui)"
test -n "$TEMPLUI_PATH"
test -d "$TEMPLUI_PATH/components"

printf '%s\n' \
  '@source "./**/*.templ";' \
  '@source "./**/*.js";' \
  "@source \"$TEMPLUI_PATH/components/**/*.templ\";" \
  "@source \"$TEMPLUI_PATH/components/**/*.js\";" \
  > ./web/assets/css/sources.generated.css

tailwindcss -i ./web/assets/css/input.css -o ./web/assets/css/output.css --minify

echo "OK: wrote web/assets/css/output.css"
