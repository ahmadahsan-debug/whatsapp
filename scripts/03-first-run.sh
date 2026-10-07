#!/usr/bin/env bash
# Step 3: run the bridge in this terminal so you can scan the QR code.
# Turns the Test lock ON first, so nothing can be sent to anyone but yourself.
source "$(dirname "$0")/lib.sh"
[ -x "$BIN" ] || { echo "Run scripts/02-build.sh first."; exit 1; }
stop_service                      # two bridges on one account would fight
cd "$BRIDGE_DIR"
mkdir -p config && touch config/test_lock
echo "Test lock is ON (sending allowed only to your own chat)."
cat <<'MSG'

On your PHONE, when the QR code appears below:
  WhatsApp > Settings > Linked devices > Link a device, then scan it.
Then leave this window open until you see history messages stop scrolling by
(a few minutes). Press Ctrl+C to stop, then run scripts/05-install-service.sh.

MSG
exec "$BIN"
