package providers

import "charm.land/catwalk/pkg/catwalk"

var CerebrasProvider = catwalk.Provider{
	ID:          "cerebras",
	Name:        "Cerebras Inference",
	APIEndpoint: "https://api.cerebras.ai/v1",
	APIKey:      "$CEREBRAS_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
