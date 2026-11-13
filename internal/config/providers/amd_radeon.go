package providers

import "charm.land/catwalk/pkg/catwalk"

var AmdRadeonProvider = catwalk.Provider{
	ID:          "amd-radeon",
	Name:        "AMD Radeon Cloud Token Factory",
	APIEndpoint: "https://developer.amd.com.cn/radeon/api/v1",
	APIKey:      "$RADEON_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
