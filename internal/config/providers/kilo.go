package providers

import "charm.land/catwalk/pkg/catwalk"

var KiloProvider = catwalk.Provider{
	ID:          "kilo",
	Name:        "Kilo Gateway",
	APIEndpoint: "https://ai-gateway.kilo.ai/api/v1",
	Type:        catwalk.TypeOpenAICompat,
}
