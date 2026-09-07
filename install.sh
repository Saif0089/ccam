#!/usr/bin/env sh
# Installs ccam for the current user only: no sudo, no /usr/local, no
# system paths anywhere. Safe to pipe straight into sh:
#
#   curl -fsSL https://raw.githubusercontent.com/Saif0089/ccam/main/install.sh | sh
set -eu

REPO="Saif0089/ccam"

os_name="$(uname -s)"
case "$os_name" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *)
    echo "ccam: unsupported OS: $os_name (this script supports macOS and Linux; see install.ps1 for Windows)" >&2
    exit 1
    ;;
esac

arch_name="$(uname -m)"
case "$arch_name" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *)
    echo "ccam: unsupported architecture: $arch_name" >&2
    exit 1
    ;;
esac

version="${CCAM_VERSION:-latest}"
asset="ccam_${os}_${arch}"
if [ "$version" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${version}/${asset}"
fi

install_dir="${CCAM_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$install_dir"

tmp="$(mktemp)"
sums="$(mktemp)"
trap 'rm -f "$tmp" "$sums"' EXIT

echo "Downloading ccam ($os/$arch)..."
curl -fsSL "$url" -o "$tmp"

# Verify against the checksums published alongside the binary. Skipped
# only if the release has none (older releases) or no sha256 tool exists.
if curl -fsSL "$(dirname "$url")/checksums.txt" -o "$sums" 2>/dev/null; then
  expected="$(grep " ${asset}\$" "$sums" | awk '{print $1}' | head -n 1)"
  if [ -n "$expected" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      actual="$(sha256sum "$tmp" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "$tmp" | awk '{print $1}')"
    fi
    if [ -n "${actual:-}" ] && [ "$actual" != "$expected" ]; then
      echo "ccam: checksum mismatch for ${asset}" >&2
      echo "  expected $expected" >&2
      echo "  actual   $actual" >&2
      exit 1
    fi
  fi
fi

# Stop any running ccam before replacing the binary. Without this an
# upgrade silently keeps serving the old build: mv swaps the file, but
# the running process holds the old inode until something restarts it.
if [ -x "$install_dir/ccam" ]; then
  "$install_dir/ccam" stop >/dev/null 2>&1 || true
fi

chmod +x "$tmp"
mv "$tmp" "$install_dir/ccam"
rm -f "$sums"
trap - EXIT

echo "Installed $install_dir/ccam"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *)
    echo ""
    echo "Note: $install_dir isn't on your PATH yet. Add this to your shell rc file:"
    echo "  export PATH=\"$install_dir:\$PATH\""
    ;;
esac

echo ""
"$install_dir/ccam" install
