// groq.go: Groq provider (free dev tier).
//
// Base URL: https://api.groq.com/openai/v1
// Auth: Bearer $GROQ_API_KEY
// Free tier: limited dev rate; very fast LPU inference.
package providers

import "charm.land/catwalk/pkg/catwalk"

var GroqProvider = catwalk.Provider{
	ID:                  "groq",
	Name:                "Groq (free dev tier, ultra-fast LPU)",
	APIEndpoint:         "https://api.groq.com/openai/v1",
	APIKey:              "$GROQ_API_KEY",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "llama-3.3-70b-versatile",
	DefaultSmallModelID: "llama-3.1-8b-instant",
}
