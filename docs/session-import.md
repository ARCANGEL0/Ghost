# Import coding agent sessions

The command picker has an **Import Agent Session** entry. Choose an agent, then
enter a session ID or part of its title or first prompt. `/import` opens the
same picker.

Ghost can read Claude Code and Codex sessions from their local session stores.
OpenCode and Kilo use their installed CLIs to list and export sessions. Those
CLIs need to be available on `PATH`. Set `CLAUDE_CONFIG_DIR` or `CODEX_HOME`
before starting Ghost if either tool uses a custom data directory.

You can also choose **Transcript file** to import an exported JSON or JSONL
transcript from another coding tool. Plain text files are imported as the first
user message.

To use the slash command directly:

```text
/import --claude <session-id or prompt>
/import --codex <session-id or prompt>
/import --opencode <session-id or title>
/import --kilo <session-id or title>
/import --file <path>
```

`/resume --<agent> ...` also imports a session from another tool. `/resume`
without an agent keeps opening Ghost's own sessions.

Import creates a new Ghost session with the readable user and assistant
messages, including text from tool calls and results where available. Ghost
then continues from that conversation. Codex sessions also keep a link to the
original thread so Ghost can resume it when you use the Codex model. Other
agents are imported as conversation history; their original runtime is not
resumed inside Ghost.
