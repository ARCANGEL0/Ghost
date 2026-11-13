package providers

import "charm.land/catwalk/pkg/catwalk"

var KiloProvider = catwalk.Provider{
	ID:                  "kilo",
	Name:                "Kilo Gateway",
	APIEndpoint:         "https://ai-gateway.kilo.ai/api/v1",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "kilo-auto/free",
	DefaultSmallModelID: "kilo-auto/free",
	Models: []catwalk.Model{{
		ID:               "kilo-auto/free",
		Name:             "Kilo Auto Free",
		ContextWindow:    128_000,
		DefaultMaxTokens: 8192,
	}},
}
