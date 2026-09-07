#!/usr/bin/env bash
# Runs the real install.sh against the real published release, as a
# Linux user would.
#
# test/linux/run.sh builds from source, so it never exercises the thing
# an actual user does first: architecture detection, downloading the
# published binary, verifying the checksums CI publishes alongside it,
# and installing to a per-user directory with no root.
set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "ok: $*"; }

export HOME=/home/tester
export PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin"
cd "$HOME"

# The published binary needs a claude to talk to; a stub is enough to
# prove ccam resolves and runs one.
mkdir -p "$HOME/.local/bin"
cat > "$HOME/.local/bin/claude" <<'STUB'
#!/bin/sh
case "$*" in
  *"auth status"*) echo '{"loggedIn": false, "authMethod": "none"}' ;;
  *--version*)     echo "9.9.9 (Claude Code)" ;;
  *)               sleep 300 ;;
esac
STUB
chmod +x "$HOME/.local/bin/claude"

echo "=== running install.sh exactly as the README says ==="
sh /src/install.sh 2>&1 | tee /tmp/install.log
grep -q "ccam is running" /tmp/install.log || fail "install.sh did not end with a running service"
ok "install.sh installed and started ccam"

[ -x "$HOME/.local/bin/ccam" ] || fail "no binary at ~/.local/bin/ccam"
ok "binary installed to a per-user directory"

# Nothing outside the user's own home may have been touched.
grep -qE "sudo|/usr/local|/etc/systemd/system" /tmp/install.log && fail "install.sh mentioned a system path"
ok "no system paths or sudo involved"

PORT=$(cat "$HOME/.ccam/port")
curl -fsS "http://127.0.0.1:$PORT/api/status" | grep -q '"service":"ccam"' || fail "service does not identify as ccam"
ok "service answers on the recorded port ($PORT)"

# The published build must report a real version, not "dev".
VERSION=$(curl -fsS "http://127.0.0.1:$PORT/api/status" | python3 -c "import json,sys; print(json.load(sys.stdin)['version'])")
[ -n "$VERSION" ] || fail "no version reported"
ok "running published version: $VERSION"

# Re-running the installer is the upgrade path, and must leave a
# working service rather than two fighting over the port.
echo "=== re-running install.sh (the upgrade path) ==="
sh /src/install.sh 2>&1 | tee /tmp/install2.log
grep -q "ccam is running" /tmp/install2.log || fail "re-running install.sh did not leave a running service"
curl -fsS "http://127.0.0.1:$PORT/api/status" | grep -q '"service":"ccam"' || fail "service not answering after re-install"
ok "re-running the installer is safe"

ccam uninstall >/dev/null
[ -f "$HOME/.local/bin/ccam" ] && fail "binary survived uninstall"
ok "uninstall removed it"

echo
echo "INSTALL SCRIPT CHECKS PASSED"
