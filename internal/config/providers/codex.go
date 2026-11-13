package providers

import "charm.land/catwalk/pkg/catwalk"

var CodexProvider = catwalk.Provider{
	ID:                  "codex",
	Name:                "Codex",
	Type:                catwalk.TypeOpenAI,
	DefaultLargeModelID: "gpt-6.1-sol",
	DefaultSmallModelID: "gpt-6.1-sol",
	Models: []catwalk.Model{{
		ID:                     "gpt-6.1-sol",
		Name:                   "GPT-6.1 Sol",
		ContextWindow:          200_000,
		DefaultMaxTokens:       8192,
		CanReason:              true,
		ReasoningLevels:        []string{"low", "medium", "high"},
		DefaultReasoningEffort: "medium",
	}},
}
