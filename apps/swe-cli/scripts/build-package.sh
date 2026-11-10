#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1:-$(git -C "$root" describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || printf 'dev')}"
dist="${DIST_DIR:-$root/dist}"
(cd "$root/../reed" && go test ./internal/installer -run Catalog)
mkdir -p "$dist"
dist="$(cd "$dist" && pwd)"

mkdir -p "$dist/bin"

targets=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)

for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  asset="swe-cli_${goos}_${goarch}"
  if [[ "$goos" == "windows" ]]; then
    asset="${asset}.exe"
  fi

  (
    cd "$root"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
      -trimpath \
      -ldflags "-s -w -X github.com/AwayerCode/ReedHarness/apps/swe-cli/internal/app.Version=${version}" \
      -o "$dist/bin/$asset" \
      .
  )
done

(
  cd "$root"
  go run ./internal/skillpackage --version "$version" --dist "$dist/bin" --out "$dist" --root ../..
)

printf 'built complete SWE Skill packages %s in %s\n' "$version" "$dist"
