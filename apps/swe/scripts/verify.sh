#!/usr/bin/env bash
# swe verification: Go, browser-script logic, source hygiene and release builds.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$root"
node_binary="${NODE_BINARY:-node}"
for executable in go git "$node_binary"; do
  command -v "$executable" >/dev/null || { echo "Missing verification dependency: $executable" >&2; exit 1; }
done

for module in apps/swe; do
  unformatted="$(find "$module" -name '*.go' -type f -print0 | xargs -0 gofmt -l)"
  if [[ -n "$unformatted" ]]; then
    echo "Go formatting required:" >&2
    echo "$unformatted" >&2
    exit 1
  fi
  (cd "$module" && go test -race ./... && go vet ./...)
done
"$node_binary" --test apps/swe/internal/systemdesign/*_test.cjs
bash -n apps/swe/scripts/verify.sh apps/swe/scripts/build-release.sh plugins/build.sh
git diff --check

# One version drives swe and the plugin bundle, because
# the plugin starts its bundled CLI with --expect-version.
version="$(git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null)"
version="${version#v}"
[[ -n "$version" ]] || version=0.0.0-dev
# The plugin bundle and its CLI must report one version, so strip the tag prefix.
apps/swe/scripts/build-release.sh "$version"
"$node_binary" plugins/package.mjs "$version"
REED_TEST_VERSION="$version" "$node_binary" --test plugins/integration.test.mjs
