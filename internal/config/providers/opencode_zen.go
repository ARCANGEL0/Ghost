package providers

import "charm.land/catwalk/pkg/catwalk"

var OpenCodeZenProvider = catwalk.Provider{
	ID:                  "opencode-zen",
	Name:                "OpenCode Zen",
	APIEndpoint:         "https://opencode.ai/zen/v1",
	APIKey:              "no-key-needed",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "big-pickle",
	DefaultSmallModelID: "deepseek-v4-flash-free",
}
