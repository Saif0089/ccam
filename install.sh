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
