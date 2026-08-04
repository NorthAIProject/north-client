#!/usr/bin/env bash
# Fail if compiled Go binaries were committed. bin/ and ad-hoc binary names are build-only.
set -euo pipefail

tracked="$(git ls-files bin/ north-web north-worker north-mcp main 2>/dev/null || true)"
if [[ -n "${tracked}" ]]; then
  echo "Build artifacts must not be tracked in git:"
  echo "${tracked}"
  exit 1
fi

echo "OK: no tracked build artifacts (bin/, north-web, north-worker, north-mcp, main)"
