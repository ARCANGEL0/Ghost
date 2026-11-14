package providers

import "charm.land/catwalk/pkg/catwalk"

var JustDoWorkProvider = catwalk.Provider{
	ID:          "justdowork",
	Name:        "JustDoWork (credits)",
	APIEndpoint: "https://api.justwoker.icu/v1",
	APIKey:      "$JUSTDOWORK_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
