#!/usr/bin/env bash
# Fail if compiled Go binaries are about to be committed or are already tracked.
set -euo pipefail

artifacts=()
if [[ "${1:-}" == "--staged" ]]; then
  files_command=(git diff --cached --name-only --diff-filter=ACM)
else
  files_command=(git ls-files)
fi

while IFS= read -r file; do
  [[ -f "$file" ]] || continue

  # `go version -m` recognizes Go binaries regardless of their filename.
  if go version -m "$file" >/dev/null 2>&1; then
    artifacts+=("$file")
  fi
done < <("${files_command[@]}")

if ((${#artifacts[@]} > 0)); then
  echo "Compiled Go binaries must not be committed:" >&2
  printf '  %s\n' "${artifacts[@]}" >&2
  exit 1
fi

echo "OK: no tracked compiled Go binaries"
