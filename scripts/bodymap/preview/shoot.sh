#!/usr/bin/env bash
# Screenshots the preview page in headless Chrome, one PNG per theme and side.
# serve.py must already be listening on :8099.
#
#   scripts/bodymap/preview/shoot.sh OUT_DIR [chrome flags...]
#
# Narrow the grid with THEMES or SIDES (space separated), pass HEAT=none for
# an untrained figure, and point at another model file with
# MODEL=/assets/models/other.glb. A clean profile keeps the person's extensions
# out of the render: --user-data-dir=/tmp/some-dir --no-first-run --disable-extensions
set -euo pipefail
out=${1:?output directory}
shift || true
mkdir -p "$out"
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
for theme in ${THEMES:-dark light}; do
  for side in ${SIDES:-front back}; do
    name="${theme}-${side}"
    "$chrome" --headless=new --hide-scrollbars --window-size=600,760 --virtual-time-budget=8000 \
      --screenshot="$out/$name.png" "$@" \
      "http://127.0.0.1:8099/?theme=$theme&side=$side${HEAT:+&heat=$HEAT}${MODEL:+&model=$MODEL}" \
      >/dev/null 2>&1 || echo "failed: $name"
    echo "$name"
  done
done
