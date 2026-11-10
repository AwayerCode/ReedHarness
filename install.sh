#!/usr/bin/env sh
set -eu

# Download, verify and run ReedHarness temporarily; leave only selected Skills in the project.
#
#   curl -fsSL https://raw.githubusercontent.com/AwayerCode/ReedHarness/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --version <release> --root ./my-project -- list

repo="AwayerCode/ReedHarness"
version="${REED_VERSION:-}"
root="."

usage() {
  cat <<'EOF'
Usage: install.sh [--version <reed-release>] [--root <dir>] -- [reed command and arguments]

  --version <tag>  Select the reed release (default: latest release)
  --root <dir>     Working directory for the command (default: current directory)
  --              Pass the remaining arguments to reed (default command: list)

The executable is removed after the command; no global or project installation.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] && [ -n "$2" ] || { usage >&2; exit 2; }
      version="$2"
      shift 2
      ;;
    --root)
      [ "$#" -ge 2 ] && [ -n "$2" ] || { usage >&2; exit 2; }
      root="$2"
      shift 2
      ;;
    --)
      shift
      break
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'install.sh: unknown argument %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    printf 'install.sh: unsupported operating system %s\n' "$(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *)
    printf 'install.sh: unsupported architecture %s\n' "$(uname -m)" >&2
    exit 1
    ;;
esac

asset="reed_${os}_${arch}"
if [ -n "$version" ]; then
  base="https://github.com/${repo}/releases/download/${version}"
else
  base="https://github.com/${repo}/releases/latest/download"
fi

cd "$root"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' 0
trap 'exit 130' INT
trap 'exit 143' TERM

printf 'downloading %s\n' "$asset" >&2
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"

expected="$(grep "  $asset\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)"
if [ -z "$expected" ]; then
  printf 'install.sh: %s is not listed in SHA256SUMS\n' "$asset" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)"
fi
if [ "$actual" != "$expected" ]; then
  printf 'install.sh: SHA256 mismatch for %s\n' "$asset" >&2
  exit 1
fi

chmod +x "$tmp/$asset"
if [ "$#" -eq 0 ]; then
  set -- list
fi
"$tmp/$asset" "$@"
