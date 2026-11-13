// cohere.go: Cohere provider (trial credits).
//
// Base URL: https://api.cohere.com/v1
// Auth: Bearer $COHERE_API_KEY
// Free: trial credits, then pay-as-you-go.
package providers

import "charm.land/catwalk/pkg/catwalk"

var CohereProvider = catwalk.Provider{
	ID:                  "cohere",
	Name:                "Cohere (trial quota)",
	APIEndpoint:         "https://api.cohere.ai/compatibility/v1",
	APIKey:              "$COHERE_API_KEY",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "north-mini-code-1.0",
	DefaultSmallModelID: "north-mini-code-1.0",
}
