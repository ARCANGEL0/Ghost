package providers

import "charm.land/catwalk/pkg/catwalk"

var TyphoonProvider = catwalk.Provider{
	ID:          "typhoon",
	Name:        "OpenTyphoon",
	APIEndpoint: "https://api.opentyphoon.ai/v1",
	APIKey:      "$OPENTYPHOON_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
