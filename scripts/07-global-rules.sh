#!/usr/bin/env bash
# Step 7: add the WhatsApp safety rules to your global ~/.claude/CLAUDE.md so every
# future Claude Code session follows them. Safe to run again: it replaces its own block.
source "$(dirname "$0")/lib.sh"
TARGET="$HOME/.claude/CLAUDE.md"
mkdir -p "$HOME/.claude"; touch "$TARGET"
BEGIN="<!-- BEGIN whatsapp-mcp rules -->"; END="<!-- END whatsapp-mcp rules -->"
BLOCK="$(sed "s#@REPO_DIR@#$REPO_DIR#g" "$REPO_DIR/docs/global-claude-md-rules.md")"
TMP="$(mktemp)"
awk -v b="$BEGIN" -v e="$END" '$0==b{skip=1} !skip{print} $0==e{skip=0}' "$TARGET" > "$TMP"
{ cat "$TMP"; printf '\n%s\n%s\n%s\n' "$BEGIN" "$BLOCK" "$END"; } > "$TARGET"
rm -f "$TMP"
echo "Rules written to $TARGET:"; sed -n "/$BEGIN/,/$END/p" "$TARGET"
