Use this tool to split research or review work across read-only agents. Each worker
has an isolated session and can inspect the project, but cannot edit files.

When `subtasks` is omitted, a worker first creates a short plan. Pass `subtasks`
when you already know which files or questions should be investigated.

Strategies:
- `parallel`: independent investigations, up to five workers at once
- `sequential`: ordered phases; each phase receives the earlier findings
- `compare`: different approaches to the same question

Keep tasks independent for `parallel`. Use `sequential` when a later phase needs
an earlier result. Read the returned evidence before making changes yourself.

Example:
```json
{
  "task": "Find why startup is slow",
  "strategy": "parallel",
  "subtasks": [
    "Trace config loading and identify expensive file reads",
    "Trace provider initialization and identify network waits",
    "Trace UI startup and identify blocking work"
  ]
}
```
