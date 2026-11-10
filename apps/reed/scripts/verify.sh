#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$root"
unformatted="$(find apps/reed -name '*.go' -type f -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then printf 'Go formatting required:\n%s\n' "$unformatted" >&2; exit 1; fi
(cd apps/reed && go test -race ./... && go vet ./...)
"${NODE_BINARY:-node}" --check apps/reed/internal/control/web/app.js
bash -n apps/reed/scripts/build-release.sh apps/reed/scripts/verify.sh
git diff --check
apps/reed/scripts/build-release.sh
