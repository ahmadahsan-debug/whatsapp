# WhatsApp bridge for Claude Code

Lets Claude Code read your WhatsApp chats, pull media and (with your approval) send messages, through MCP tools. Everything runs on your own computer; messages reach Claude only when a tool is called.

Based on [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) (MIT), with a current whatsmeow, safety locks, health check and private logging.

## Setup (macOS or Linux)
Run these from this folder, in order. `CLAUDE.md` explains what each does.
```
scripts/01-install-tools.sh
scripts/02-build.sh
scripts/03-first-run.sh        # scan the QR code on your phone
scripts/04-register-mcp.sh     # then restart Claude Code
scripts/05-install-service.sh  # always-on background service
scripts/07-global-rules.sh     # safety rules for every Claude session
scripts/06-install-health-check.sh   # optional
```
Day to day: `scripts/status.sh` (is it healthy?) and `scripts/restart.sh`.
The Test lock starts ON (sends only to yourself). When you are happy with the test message, run `scripts/test-lock.sh off`; Claude still asks you before every send.
If you get logged out: see **Troubleshooting** in `CLAUDE.md`.

**Your private data lives in `whatsapp-bridge/store/` and is never committed** (see `.gitignore`). Keep this repository private.
