#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1:-$(git -C "$root" describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || printf 'dev')}"
dist="${DIST_DIR:-$root/dist}"
mkdir -p "$dist"
dist="$(cd "$dist" && pwd)"

# Rebuild only this run's assets; keep bin/ and plugins/ for reuse.
rm -f "$dist"/reed_* "$dist"/swe-*.tar.gz "$dist"/swe_*.tar.gz "$dist"/SHA256SUMS

(cd "$root" && go generate ./...)

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
  asset="reed_${goos}_${goarch}"
  if [[ "$goos" == "windows" ]]; then
    asset="${asset}.exe"
  fi

  (
    cd "$root"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
      -trimpath \
      -ldflags "-s -w -X github.com/AwayerCode/ReedHarness/apps/reed/internal/app.Version=${version}" \
      -o "$dist/bin/$asset" \
      .
  )
done

(
  cd "$root"
  go run ./internal/skillpackage --version "$version" --dist "$dist/bin" --out "$dist" --root ../..
)

printf 'built independent CLI and Skill packages %s in %s\n' "$version" "$dist"
