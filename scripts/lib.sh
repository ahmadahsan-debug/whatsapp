#!/usr/bin/env bash
# Shared settings for the helper scripts. Works on macOS and Linux.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRIDGE_DIR="$REPO_DIR/whatsapp-bridge"
BIN="$BRIDGE_DIR/bin/whatsapp-bridge"
LABEL="com.whatsapp-mcp.bridge"
OS="$(uname -s)"

case "$OS" in
  Darwin) PLIST="$HOME/Library/LaunchAgents/$LABEL.plist" ;;
  Linux)  UNIT="$HOME/.config/systemd/user/whatsapp-bridge.service" ;;
  *) echo "This helper supports macOS and Linux only (found: $OS). On Windows, ask Claude to adapt it."; exit 1 ;;
esac

stop_service() {   # harmless if the service is not installed
  if [ "$OS" = Darwin ]; then
    launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  else
    systemctl --user stop whatsapp-bridge 2>/dev/null || true
  fi
}

start_service() {
  if [ "$OS" = Darwin ]; then
    launchctl bootstrap "gui/$(id -u)" "$PLIST" 2>/dev/null || true
    launchctl kickstart -k "gui/$(id -u)/$LABEL"
  else
    systemctl --user daemon-reload
    systemctl --user enable --now whatsapp-bridge
    systemctl --user restart whatsapp-bridge
  fi
}
