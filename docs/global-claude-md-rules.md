## WhatsApp bridge rules (apply in every project)

The WhatsApp bridge and MCP server live in `@REPO_DIR@`.

1. **Show before you send.** Before sending any WhatsApp message as me (text, file or voice note), show me the exact text/file and the recipient, and send only after I say yes to that one message. A yes for one message is not a yes for the next. Reading, searching and drafting need no approval.
2. **Test sends go to my own chat, with the Test lock on.** While testing, only send to my own number ("Message yourself"), never to anyone else. Keep `whatsapp-bridge/config/test_lock` in place whenever testing or changing the bridge.
3. **Never touch `whatsapp-bridge/store/`.** It holds my message history (`messages.db`) and my login keys (`whatsapp.db`). Don't move, edit, delete or commit anything in it. If something seems to need that, stop and ask me.
4. **Download voice notes only when their content is needed.** Never in bulk: every download goes through my phone and drains its battery.
5. **"Client outdated (405)"** means WhatsApp rejected the old client. Fix: in `whatsapp-bridge/` run `go get go.mau.fi/whatsmeow@latest && go mod tidy`, rebuild with `scripts/02-build.sh`, then restart with `scripts/restart.sh`.
6. **Read `@REPO_DIR@/CLAUDE.md` before changing the bridge.** It explains how it is built, run, restarted and tested.
