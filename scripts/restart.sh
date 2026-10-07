#!/usr/bin/env bash
# Restart the background bridge (e.g. after rebuilding it).
source "$(dirname "$0")/lib.sh"
start_service
sleep 3
curl -s http://127.0.0.1:8080/api/health; echo
