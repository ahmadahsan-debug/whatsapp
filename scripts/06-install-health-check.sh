#!/usr/bin/env bash
# Optional step 6: check twice a day (09:00 and 18:00) and notify only on problems.
source "$(dirname "$0")/lib.sh"
CHECK="$REPO_DIR/scripts/health-check.sh"; chmod +x "$CHECK"
if [ "$OS" = Darwin ]; then
  P="$HOME/Library/LaunchAgents/$LABEL.healthcheck.plist"
  cat > "$P" <<PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL.healthcheck</string>
  <key>ProgramArguments</key><array><string>/bin/bash</string><string>$CHECK</string></array>
  <key>StartCalendarInterval</key>
  <array>
    <dict><key>Hour</key><integer>9</integer><key>Minute</key><integer>0</integer></dict>
    <dict><key>Hour</key><integer>18</integer><key>Minute</key><integer>0</integer></dict>
  </array>
</dict>
</plist>
PLISTEOF
  launchctl bootout "gui/$(id -u)/$LABEL.healthcheck" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$P"
else
  D="$HOME/.config/systemd/user"; mkdir -p "$D"
  printf '[Unit]\nDescription=WhatsApp bridge health check\n[Service]\nType=oneshot\nExecStart=/bin/bash %s\n' "$CHECK" > "$D/whatsapp-bridge-health.service"
  printf '[Unit]\nDescription=WhatsApp bridge health check twice daily\n[Timer]\nOnCalendar=*-*-* 09:00:00\nOnCalendar=*-*-* 18:00:00\nPersistent=true\n[Install]\nWantedBy=timers.target\n' > "$D/whatsapp-bridge-health.timer"
  systemctl --user daemon-reload; systemctl --user enable --now whatsapp-bridge-health.timer
fi
echo "Health check scheduled for 09:00 and 18:00."
