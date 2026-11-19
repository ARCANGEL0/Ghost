package providers

import "charm.land/catwalk/pkg/catwalk"

var ClaudeCodeProvider = catwalk.Provider{
	ID:                  "claude-code",
	Name:                "Claude Code",
	Type:                catwalk.TypeAnthropic,
	DefaultLargeModelID: "account-default",
	DefaultSmallModelID: "account-default",
	Models: []catwalk.Model{{
		ID:               "account-default",
		Name:             "Claude Code (account default)",
		ContextWindow:    200_000,
		DefaultMaxTokens: 16_384,
		CanReason:        true,
	}},
}
