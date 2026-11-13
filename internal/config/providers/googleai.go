package providers

import "charm.land/catwalk/pkg/catwalk"

var GoogleAIProvider = catwalk.Provider{
	ID:                  "googleai",
	Name:                "Google AI Studio (Gemini, free tier)",
	APIEndpoint:         "https://generativelanguage.googleapis.com/v1beta/openai",
	APIKey:              "$GEMINI_API_KEY",
	Type:                catwalk.TypeOpenAICompat,
	DefaultLargeModelID: "gemini-3.5-flash",
	DefaultSmallModelID: "gemini-3.5-flash-lite",
}
