Use this tool to split a task across agents. Planning stays read only. Each task
worker gets a separate Git worktree and may edit files there. Changes stay in the
worktree for review and do not change the shared project checkout.

When `subtasks` is omitted, a read only worker first creates a short plan. Pass
`subtasks` when you already know which work should be split up.

Strategies:
- `parallel`: independent investigations, up to five workers at once
- `sequential`: ordered phases; each phase receives the earlier findings
- `compare`: different approaches to the same question

Keep tasks independent for `parallel`. Use `sequential` when a later phase needs
an earlier result. Review each worktree's changes before integrating them.
The worker result includes its worktree path and changed files. Remove a worktree
after review with `git worktree remove <path>`.

Example:
```json
{
  "task": "Fix the slow startup path",
  "strategy": "parallel",
  "subtasks": [
    "Measure and improve config startup",
    "Reduce provider initialization waits",
    "Find and fix blocking UI startup work"
  ]
}
```
