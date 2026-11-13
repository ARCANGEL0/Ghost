# Session import

Ghost can import a Claude Code transcript into a new local session. The import
keeps user and assistant messages, along with readable tool calls and results.
It leaves the original Claude Code session untouched.

In Ghost, run:

```text
/import --claude <session-id>
```

`/resume --claude <session-id>` does the same thing. Ghost reads the matching
JSONL file from `~/.claude/projects`. If Claude Code uses a custom config path,
set `CLAUDE_CONFIG_DIR` before starting Ghost.

After import, choose any configured Ghost model and continue the conversation.
