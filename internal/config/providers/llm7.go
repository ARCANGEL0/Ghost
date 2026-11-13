package providers

import "charm.land/catwalk/pkg/catwalk"

var LLM7Provider = catwalk.Provider{
	ID:          "llm7",
	Name:        "LLM7",
	APIEndpoint: "https://api.llm7.io/v1",
	APIKey:      "$LLM7_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
