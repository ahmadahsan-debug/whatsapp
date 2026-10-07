#!/usr/bin/env bash
# Optional scheduled check. Silent when all is well; shows a desktop
# notification ONLY when the bridge is down or logged out.
URL="http://127.0.0.1:8080/api/health"
notify() {
  if [ "$(uname -s)" = Darwin ]; then
    osascript -e "display notification \"$1\" with title \"WhatsApp bridge\"" >/dev/null 2>&1
  else
    command -v notify-send >/dev/null && notify-send "WhatsApp bridge" "$1"
  fi
}
status() { curl -s --max-time 5 "$URL" | sed -n 's/.*"status":"\([a-z_]*\)".*/\1/p'; }

s="$(status)"
if [ "$s" = "connecting" ]; then sleep 120; s="$(status)"; fi   # reconnecting is normal; only a long stall counts
case "$s" in
  ok) exit 0 ;;
  logged_out) notify "Logged out of WhatsApp. Scan a new QR code (see CLAUDE.md in whatsapp-mcp)." ;;
  "") notify "The bridge is not running. Run scripts/restart.sh." ;;
  *)  notify "The bridge is stuck connecting. Run scripts/restart.sh." ;;
esac
