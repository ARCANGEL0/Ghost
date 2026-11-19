# Claude Code ACP

Ghost runs the installed Claude Code CLI through the
[`claude-agent-acp`](https://github.com/agentclientprotocol/claude-agent-acp)
adapter. Claude Code handles the account, model access, and billing.

## Check the account

```text
ghost login claude
```

This checks that Claude Code is installed, signed in to a first-party account,
and can complete a short Haiku request. It uses a small amount of the account's
usage. If Claude Code needs sign-in, run:

```text
claude auth login
```

## Use Claude Code in Ghost

Choose **Claude Code** in the provider list, then select **Claude Code (account
default)**. Ghost checks the CLI login before starting a run. Each Ghost session
uses the matching Claude Code ACP session when it is available.

If `claude-agent-acp` is not installed, Ghost launches the pinned package with
`npx`. This requires Node.js 22 or newer and downloads the adapter the first
time it is used. Ghost points the adapter at the installed `claude` executable
and removes API-key environment variables from the adapter process.

## Anthropic API keys

For direct Anthropic API access, configure the **Anthropic** provider with an
API key. Old subscription tokens stored under that provider are no longer used:
select **Claude Code** to use the local CLI account.

If a request fails after the login check, Claude Code returns the account or
service error. A successful login check cannot guarantee remaining quota.
