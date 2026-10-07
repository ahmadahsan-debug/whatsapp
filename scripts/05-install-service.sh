#!/usr/bin/env bash
# Step 5: run the bridge in the background: starts at login, restarts if it crashes.
# Logs stay in whatsapp-bridge/logs (daily files, deleted after 14 days; no message text).
source "$(dirname "$0")/lib.sh"
[ -x "$BIN" ] || { echo "Run scripts/02-build.sh first."; exit 1; }
[ -f "$BRIDGE_DIR/store/whatsapp.db" ] || { echo "Not linked to WhatsApp yet. Run scripts/03-first-run.sh and scan the QR code first."; exit 1; }
mkdir -p "$BRIDGE_DIR/logs"

if [ "$OS" = Darwin ]; then
  mkdir -p "$(dirname "$PLIST")"
  cat > "$PLIST" <<PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BIN</string></array>
  <key>WorkingDirectory</key><string>$BRIDGE_DIR</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <!-- The bridge writes its own daily log files; stdout would only duplicate them. -->
  <key>StandardOutPath</key><string>/dev/null</string>
  <key>StandardErrorPath</key><string>$BRIDGE_DIR/logs/crash-stderr.txt</string>
</dict>
</plist>
PLISTEOF
else
  mkdir -p "$(dirname "$UNIT")"
  cat > "$UNIT" <<UNITEOF
[Unit]
Description=WhatsApp bridge (local only)

[Service]
ExecStart=$BIN
WorkingDirectory=$BRIDGE_DIR
Restart=always
RestartSec=10
StandardOutput=null
StandardError=append:$BRIDGE_DIR/logs/crash-stderr.txt

[Install]
WantedBy=default.target
UNITEOF
fi
start_service
sleep 4
curl -s http://127.0.0.1:8080/api/health || echo "(not answering yet; check again in a few seconds)"
echo
