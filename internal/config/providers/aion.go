package providers

import "charm.land/catwalk/pkg/catwalk"

var AionProvider = catwalk.Provider{
	ID:                  "aion",
	Name:                "Aion Labs (daily credits)",
	APIEndpoint:         "https://api.aionlabs.ai/v1",
	APIKey:              "$AION_API_KEY",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "aion-labs/aion-3.5-mini",
}
