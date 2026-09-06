#!/usr/bin/env sh
# Installs ccam for the current user only: no sudo, no /usr/local, no
# system paths anywhere. Safe to pipe straight into sh:
#
#   curl -fsSL https://ccam-six.vercel.app/install.sh | sh
#
# Binaries are served from this same site (public/releases in the repo,
# published by .github/workflows/release.yml) rather than GitHub
# Releases, since the source repo is private.
set -eu

BASE_URL="https://ccam-six.vercel.app"

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

asset="ccam_${os}_${arch}"
url="${BASE_URL}/releases/${asset}"

install_dir="${CCAM_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$install_dir"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Downloading ccam ($os/$arch)..."
curl -fsSL "$url" -o "$tmp"
chmod +x "$tmp"
mv "$tmp" "$install_dir/ccam"
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
