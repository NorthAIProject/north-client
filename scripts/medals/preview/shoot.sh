#!/usr/bin/env bash
# Screenshots the medals page in headless Chrome, full page, light and dark.
# serve.py must already be listening on :8098 (go run ./scripts/medals first).
#
#   scripts/medals/preview/shoot.sh OUT_DIR [chrome flags...]
set -euo pipefail
out=${1:?output directory}
shift || true
mkdir -p "$out"
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
for theme in ${THEMES:-light dark}; do
  "$chrome" --headless=new --hide-scrollbars --window-size=1150,${HEIGHT:-1500} --virtual-time-budget=3000 \
    --user-data-dir="${TMPDIR:-/tmp}/medals-chrome" --no-first-run --disable-extensions \
    --screenshot="$out/$theme.png" "$@" \
    "http://127.0.0.1:8098/?theme=$theme" >/dev/null 2>&1 || echo "failed: $theme"
  echo "$theme"
done
