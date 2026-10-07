#!/usr/bin/env bash
# Step 2: compile the bridge into a binary (the background service runs this binary, never "go run").
source "$(dirname "$0")/lib.sh"
cd "$BRIDGE_DIR"
mkdir -p bin
go build -o bin/whatsapp-bridge .
echo "Built: $BIN"
