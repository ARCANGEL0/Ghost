# Agent runs

Open **Agent Runs** from the command list or enter `/runs`. The view updates
while Ghost is working and shows the model, elapsed time, tool calls, retries,
tokens, and estimated cost for each agent.

Use `p` to pause or resume, `c` to cancel, and `r` to retry a completed top
level run. Pause takes effect between model steps; it does not interrupt a
request or tool call already in progress. Retrying repeats the saved prompt in
the same session.
