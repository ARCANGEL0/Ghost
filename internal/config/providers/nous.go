package providers

import "charm.land/catwalk/pkg/catwalk"

var NousProvider = catwalk.Provider{
	ID:          "nous-portal",
	Name:        "Nous Portal",
	APIEndpoint: "https://inference-api.nousresearch.com/v1",
	APIKey:      "$NOUS_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
