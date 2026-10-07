# WhatsApp bridge: notes for Claude (read before changing anything)

Two programs run on the user's own computer. Messages stay local; they reach Claude only when an MCP tool is called.

| Piece | Where | What it does |
|---|---|---|
| **Bridge** (Go) | `whatsapp-bridge/` | Logs into WhatsApp as a linked device (whatsmeow), keeps messages in SQLite, serves an HTTP API on **127.0.0.1:8080 only**. |
| **MCP server** (Python) | `whatsapp-mcp-server/` | Started by Claude Code (stdio). Reads `messages.db` read-only and calls the bridge API to send/download. |

## Hard rules
- **Never touch `whatsapp-bridge/store/`** (`messages.db` = history, `whatsapp.db` = login keys, plus downloaded media). Don't move, edit, delete, copy out or commit. If a task seems to need it, stop and ask the user.
- **Never commit** `store/`, `*.db`, media, `logs/`, `config/test_lock`, `bin/`. Before every push run `git diff --cached --name-only` and read the list.
- **Show before you send**: show exact text + recipient, send only after the user says yes to that message.
- **Keep the Test lock on while testing/changing the bridge** (`touch whatsapp-bridge/config/test_lock`). Test sends go to the user's own number only.
- **The API must stay on 127.0.0.1.** Never `0.0.0.0` or a bare `:8080`.
- **Never log message text, captions or file names.** Log who, where, when, type, message ID only (`appLog` in `logging.go`).
- Don't download voice notes/media in bulk (each goes through the phone).

## Code map (`whatsapp-bridge/`)
- `main.go`: startup, QR pairing (terminal only), event handlers, shutdown.
- `server.go`: HTTP API (`/api/send`, `/api/download`, `/api/health`), loopback + JSON-only request guard, `loggedOut` flag.
- `send.go`: **the only send path** (`Sender.Send`): Test lock, Repeat lock, connect check, upload/send, Send record.
- `store.go`: SQLite tables `chats`, `messages`, `sent_log`.
- `messages.go`: incoming message + history-sync handling, chat names. `media.go`: downloads. `oggopus.go`: voice-note duration/waveform.
- `logging.go`: daily log files `logs/bridge-YYYY-MM-DD.log`, 14-day cleanup, whatsmeow logger adapter (debug dropped).
- `send_test.go`: tests for the locks, send record, recipient parsing, log pruning, request guard.

## Safety features
- **Test lock**: while file `whatsapp-bridge/config/test_lock` exists, every send to anyone but the user's own number is refused (HTTP 403). Checked on every send, no restart needed. Unknown file errors count as "locked".
- **Repeat lock**: identical text (+file contents) to the same chat within 24 h is refused (HTTP 409) unless the call has `deliberate_resend=true`. Based on `sent_log.content_hash`.
- **Send record**: every send is stored in `sent_log` and also in `messages` (is_from_me), so it appears when the chat is read back.
- **Request guard**: Host must be loopback; POST must be `application/json` (stops a web page in the user's browser from triggering sends).
- **Health**: `GET /api/health` → `{connected, logged_in, status, test_lock}`. `status`: `ok` | `connecting` (temporary) | `logged_out` (only this one needs a new QR scan). The MCP tool `bridge_health` adds `bridge_not_running`.

## Build, run, restart, test
All helper scripts are in `scripts/` (macOS launchd, Linux systemd --user). Paths come from the repo location.
```
scripts/01-install-tools.sh     # go, uv, ffmpeg, git
scripts/02-build.sh             # go build -> whatsapp-bridge/bin/whatsapp-bridge (service uses this binary, never `go run`)
scripts/03-first-run.sh         # Test lock ON, run in foreground, scan QR (Settings > Linked devices > Link a device)
scripts/04-register-mcp.sh      # claude mcp add whatsapp --scope user ... ; then restart Claude Code
scripts/05-install-service.sh   # launchd/systemd: start at login, restart on crash
scripts/restart.sh              # restart after rebuilding
scripts/status.sh               # health + last log lines
scripts/06-install-health-check.sh  # optional: 09:00/18:00 notification only when down/logged out
scripts/07-global-rules.sh      # writes the rules in docs/global-claude-md-rules.md to ~/.claude/CLAUDE.md
```
After changing Go code: `cd whatsapp-bridge && gofmt -l . && go vet ./... && go test ./...`, then `scripts/02-build.sh && scripts/restart.sh`. Tests use fake connections in temp dirs and never touch real WhatsApp or `store/`.
Working directory matters: the bridge reads/writes `store/`, `config/`, `logs/` relative to `whatsapp-bridge/` (the service sets it).

## Troubleshooting
- **"Client outdated (405)"**: `cd whatsapp-bridge && go get go.mau.fi/whatsmeow@latest && go mod tidy`, fix any compile errors (the library changes signatures, e.g. added `context.Context`), rebuild, restart.
- **Health says `logged_out`**: the user must re-link. `scripts/03-first-run.sh` (stops the service, shows a QR in the terminal), scan, Ctrl+C, `scripts/05-install-service.sh`. The old `store/` stays in place; if linking keeps failing, ask the user before doing anything to `store/`.
- **`bridge_not_running`**: `scripts/status.sh`, then `scripts/restart.sh`; check `logs/` (and `logs/crash-stderr.txt`).
- **Port 8080 busy / two bridges**: only one bridge may run per account; stop the service before running manually.
- Log lines intentionally contain no message text; don't add any.
