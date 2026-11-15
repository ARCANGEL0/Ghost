# Delegated model routing

Ghost keeps the selected large and small models as its routing pool. A task
agent uses the large model for implementation and debugging. Planning tasks use
the small model when its context window fits. Research tasks may use the small
model when its provider supports tool calls and its estimated cost or recent
latency is better.

Context and cost estimates are based on the task text and catalog pricing.
Latency averages are kept in memory for the current Ghost process. The model
shown in **Agent Runs** is the one selected for that task.
