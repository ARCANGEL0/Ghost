// openrouter_free.go: OpenRouter provider pre-configured for free models.
//
// Base URL: https://openrouter.ai/api/v1
// Auth: Bearer $OPENROUTER_API_KEY
// Free models are tagged with :free suffix in OpenRouter.
package providers

import "charm.land/catwalk/pkg/catwalk"

var OpenRouterFreeProvider = catwalk.Provider{
	ID:          "openrouter",
	Name:        "OpenRouter Free",
	APIEndpoint: "https://openrouter.ai/api/v1",
	APIKey:      "$OPENROUTER_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
