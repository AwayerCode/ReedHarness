#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1:?explicit release version required}"
bash "$root/apps/reed/scripts/build-release.sh" "$version"
node "$root/plugins/package.mjs" "$version"
