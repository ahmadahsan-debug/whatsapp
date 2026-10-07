#!/usr/bin/env bash
# Is the bridge healthy? Prints the health JSON and today's last log lines.
source "$(dirname "$0")/lib.sh"
echo "Health:"; curl -s --max-time 5 http://127.0.0.1:8080/api/health || echo "NOT RUNNING (try scripts/restart.sh)"
echo; echo "Last log lines:"
tail -n 8 "$BRIDGE_DIR/logs/bridge-$(date +%F).log" 2>/dev/null || echo "(no log today)"
