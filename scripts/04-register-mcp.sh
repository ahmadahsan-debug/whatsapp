#!/usr/bin/env bash
# Step 4: register the MCP server with Claude Code for all projects (user scope).
source "$(dirname "$0")/lib.sh"
command -v claude >/dev/null || { echo "The 'claude' command was not found. Install Claude Code first."; exit 1; }
UV="$(command -v uv)"
claude mcp remove whatsapp --scope user 2>/dev/null || true
claude mcp add whatsapp --scope user -- "$UV" --directory "$REPO_DIR/whatsapp-mcp-server" run main.py
echo; echo "Done. Now QUIT and restart Claude Code so it picks up the new tools."
