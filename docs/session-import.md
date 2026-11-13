# Session import

Ghost can import Claude Code and Codex transcripts into a new local session.
The import keeps user and assistant messages, along with readable tool calls
and results. It leaves the original session untouched.

In Ghost, run:

```text
/import --claude <session-id>
/import --codex <session-id>
```

`/resume --claude <session-id>` and `/resume --codex <session-id>` are aliases.
Ghost reads Claude Code transcripts from `~/.claude/projects` and Codex
transcripts from `~/.codex/sessions`. Set `CLAUDE_CONFIG_DIR` or `CODEX_HOME`
before starting Ghost if either tool uses a custom config path.

Import creates a Ghost session with the transcript. Other Ghost models continue
from the imported messages. When you choose the Codex model, Ghost resumes the
linked Codex thread through the installed Codex CLI.
