# Agent hooks

Hooks run a command at selected points in an agent session. Add them under
`options.hooks` in `Ghost.json`:

```json
{
  "options": {
    "hooks": [
      {
        "event": "post_tool_use",
        "matcher": "edit",
        "command": "golangci-lint run ./...",
        "timeout_seconds": 30
      }
    ]
  }
}
```

Supported events are `session_start`, `user_prompt_submit`, `pre_tool_use`,
`post_tool_use`, `pre_compact`, and `stop`. Tool events accept a `matcher`
pattern such as `edit` or `mcp_*`. An empty matcher applies to every tool.

Ghost sends a JSON object to the command's standard input. It includes the
event, session ID, prompt, tool name, tool input, tool output, and error state
when those values apply. The event and session ID are also available as
`GHOST_HOOK_EVENT` and `GHOST_SESSION_ID` environment variables.

Only `pre_tool_use` can block an action. Return `{"decision":"deny"}` and an
optional `reason` on standard output, or exit with an error. Other hook errors
are logged and the session continues. Hook commands run from the project
directory and time out after 10 seconds by default, with a 60 second maximum.
