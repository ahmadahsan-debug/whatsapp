#!/usr/bin/env bash
# Switch the Test lock: on | off | status. Takes effect immediately, no restart.
# ON  = the bridge only sends to your own chat.   OFF = normal sending (still needs your yes per message).
source "$(dirname "$0")/lib.sh"
F="$BRIDGE_DIR/config/test_lock"
case "${1:-status}" in
  on)  mkdir -p "$BRIDGE_DIR/config"; touch "$F"; echo "Test lock is ON: sends only to your own chat." ;;
  off) rm -f "$F"; echo "Test lock is OFF: the bridge can send to anyone (Claude must still ask you first)." ;;
  status) [ -f "$F" ] && echo "Test lock is ON" || echo "Test lock is OFF" ;;
  *) echo "usage: $0 on|off|status"; exit 1 ;;
esac
