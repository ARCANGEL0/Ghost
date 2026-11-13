package providers

import "charm.land/catwalk/pkg/catwalk"

var AutoFreeProvider = catwalk.Provider{
	ID:                  "auto-free",
	Name:                "AUTO:FREE",
	APIKey:              "no-key-needed",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "auto:free",
	DefaultSmallModelID: "auto:free",
	Models: []catwalk.Model{{
		ID:               "auto:free",
		Name:             "AUTO:FREE",
		ContextWindow:    128_000,
		DefaultMaxTokens: 8192,
		SupportsImages:   true,
	}},
}
