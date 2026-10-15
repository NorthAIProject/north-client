#!/usr/bin/env bash
# Screenshots the preview page in headless Chrome, one PNG per combination of
# variant, theme, shell opacity and side. serve.py must already be listening.
#
#   scripts/bodymap/preview/shoot.sh OUT_DIR [chrome flags...]
#
# Narrow the grid with VARIANTS, THEMES, SHELLS or SIDES (space separated), and
# point at another model file with MODEL=/assets/models/other.glb.
set -euo pipefail
out=${1:?output directory}
shift || true
mkdir -p "$out"
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
for variant in ${VARIANTS:-a b}; do
  for theme in ${THEMES:-dark light}; do
    for shell in ${SHELLS:-0.15 0.3}; do
      for side in ${SIDES:-front back}; do
        name="${variant}-${theme}-shell${shell}-${side}"
        "$chrome" --headless=new --hide-scrollbars --window-size=600,760 --virtual-time-budget=8000 \
          --screenshot="$out/$name.png" "$@" \
          "http://127.0.0.1:8099/?variant=$variant&theme=$theme&shell=$shell&side=$side${MODEL:+&model=$MODEL}" \
          >/dev/null 2>&1 || echo "failed: $name"
        echo "$name"
      done
    done
  done
done
