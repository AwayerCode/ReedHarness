#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1:-$(git -C "$root" describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || printf 'dev')}"
dist="${DIST_DIR:-$root/dist}"
mkdir -p "$dist"
dist="$(cd "$dist" && pwd)"
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  goos="${target%/*}"
  goarch="${target#*/}"
  package="$staging/$goos-$goarch/reed"
  mkdir -p "$package/bin"
  binary=reed
  [[ "$goos" != windows ]] || binary=reed.exe
  (cd "$root" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "-s -w -X main.Version=$version" -o "$package/bin/$binary" .)
  cp "$root/README.md" "$package/README.md"
  tar -czf "$dist/reed_${goos}_${goarch}.tar.gz" -C "$(dirname "$package")" reed
 done
(cd "$dist" && shasum -a 256 reed_*.tar.gz > SHA256SUMS)
printf 'built reed service %s in %s\n' "$version" "$dist"
