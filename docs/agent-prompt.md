# Agent prompt

Ghost keeps its base role prompts in `internal/agent/templates`. The engineering
guide in `internal/agent/templates/ghost_engineering.md.tpl` is added to agents
built by the coordinator; it does not replace `coder.md.tpl`.

The guide covers repository discovery, scoped changes, swarm ownership, and
verification after a logical group of edits. The active role still controls
whether an agent can make changes.
