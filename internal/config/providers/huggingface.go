package providers

import "charm.land/catwalk/pkg/catwalk"

var HuggingFaceProvider = catwalk.Provider{
	ID:                  "huggingface",
	Name:                "Hugging Face Inference Providers (monthly credits)",
	APIEndpoint:         "https://router.huggingface.co/v1",
	APIKey:              "$HF_TOKEN",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "openai/gpt-oss-120b:fastest",
}
