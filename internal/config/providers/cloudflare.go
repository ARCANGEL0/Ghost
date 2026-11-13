package providers

import "charm.land/catwalk/pkg/catwalk"

var CloudflareProvider = catwalk.Provider{
	ID:                  "cloudflare",
	Name:                "Cloudflare Workers AI",
	APIEndpoint:         "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/ai/v1",
	APIKey:              "$CLOUDFLARE_API_KEY",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "@cf/nvidia/nemotron-3-120b-a12b",
	DefaultSmallModelID: "@cf/zai-org/glm-4.7-flash",
}
