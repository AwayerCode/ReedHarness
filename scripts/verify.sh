#!/usr/bin/env bash
# Repository verification: Go, browser-script logic, source hygiene and release builds.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
node_binary="${NODE_BINARY:-node}"
python_binary="${PYTHON_BINARY:-python3}"
for executable in go git "$node_binary" "$python_binary"; do
  command -v "$executable" >/dev/null || { echo "Missing verification dependency: $executable" >&2; exit 1; }
done

for module in apps/reed apps/swe-cli; do
  unformatted="$(find "$module" -name '*.go' -type f -print0 | xargs -0 gofmt -l)"
  if [[ -n "$unformatted" ]]; then
    echo "Go formatting required:" >&2
    echo "$unformatted" >&2
    exit 1
  fi
  (cd "$module" && go test -race ./... && go vet ./...)
done
"$node_binary" --test apps/swe-cli/internal/systemdesign/*_test.cjs
sh -n install.sh
bash -n scripts/verify.sh apps/reed/scripts/build-release.sh apps/swe-cli/scripts/build-package.sh
"$python_binary" -B scripts/knowledge_lint.py --changed
git diff --check
apps/reed/scripts/build-release.sh
apps/swe-cli/scripts/build-package.sh
