#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1:-$(git -C "$root" describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || printf 'dev')}"
dist="${DIST_DIR:-$root/dist}"
mkdir -p "$dist"
dist="$(cd "$dist" && pwd)"
cli_version="${CLI_VERSION:-$version}"

# A release holds only assets built by this run; stale packages never ship.
rm -f "$dist"/reed_* "$dist"/swe-cli_* "$dist"/swe-* "$dist"/swe_* "$dist"/SHA256SUMS

targets=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)
assets=()

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
      -ldflags "-s -w -X github.com/AwayerCode/ReedHarness/apps/reed/internal/buildinfo.Version=${version}" \
      -o "$dist/$asset" \
      .
  )
  assets+=("$asset")
done

# A release carries separate installer, CLI and text Skill assets.
package_stage="$(mktemp -d)"
trap 'rm -rf "$package_stage"' EXIT
DIST_DIR="$package_stage" "$root/../swe-cli/scripts/build-package.sh" "$cli_version"
for target in "${targets[@]}"; do
  package="swe-cli_${target%/*}_${target#*/}.tar.gz"
  cp "$package_stage/$package" "$dist/$package"
  assets+=("$package")
done

for skill in "$package_stage"/swe-*.tar.gz; do
  package="$(basename "$skill")"
  [[ "$package" == swe-cli_* ]] && continue
  cp "$skill" "$dist/$package"
  assets+=("$package")
done

(
  cd "$dist"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${assets[@]}" >SHA256SUMS
  else
    shasum -a 256 "${assets[@]}" >SHA256SUMS
  fi
)

printf 'built ReedHarness %s (CLI %s) in %s\n' "$version" "$cli_version" "$dist"
