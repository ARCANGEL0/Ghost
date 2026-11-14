package providers

import "charm.land/catwalk/pkg/catwalk"

var CodeCraftProvider = catwalk.Provider{
	ID:          "codecraft",
	Name:        "CodeCraft (free monthly quota)",
	APIEndpoint: "https://www.codecraftapi.com/v1",
	APIKey:      "$CODECRAFT_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
