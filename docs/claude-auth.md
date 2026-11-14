# Claude OAuth

Ghost can use a Claude Pro/Max subscription instead of an Anthropic API key.
Authentication happens through Claude Code's public OAuth client, and requests
are billed to the subscription.

## Sign in

```text
ghost login claude
```

Ghost first looks for an existing Claude Code sign-in:

- `~/.claude/.credentials.json` (honors `CLAUDE_CONFIG_DIR`)
- the macOS keychain entry `Claude Code-credentials`

If one is found, Ghost imports it and exits. Otherwise it opens the Claude
authorization page, asks for the code Claude displays, and stores the returned
token under `providers.anthropic` in the global config.

The pasted code has the form `code#state`; both halves are required.

## How requests are sent

When `providers.anthropic` carries an OAuth token, Ghost sends:

```text
Authorization: Bearer <access-token>
anthropic-beta: oauth-2025-04-20
```

It also prepends the Claude Code identity line to the system prompt, which the
subscription endpoint expects. The API key path is untouched, so `x-api-key`
providers keep working.

## Refresh

Access tokens are short-lived. Ghost refreshes them automatically before a run
when the token is near expiry, and retries once after a `401`. Anthropic rotates
the refresh token, and Ghost persists the rotated value.

If the token came from a Claude Code credentials file, Ghost writes the rotated
token back to that file, so the Claude Code CLI stays signed in. Ghost only
writes when the file still holds the refresh token it started from, so a newer
Claude Code sign-in is never overwritten.

## Going back to an API key

```text
ghost login anthropic
```

Saving an API key clears the stored OAuth token for the provider. The next run
uses `x-api-key` again.
